package vrchat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	federationcore "prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

const (
	stepIdentify       = "identify"
	stepProof          = "proof"
	maxProviderBackoff = 15 * time.Minute
	defaultRetryAfter  = time.Minute
)

type proofClient interface {
	PublicUser(context.Context, string, []http.Cookie) (PublicUser, []http.Cookie, error)
	DecodeCookies([]byte, time.Time) ([]http.Cookie, error)
}

type proofSecretStore interface {
	OpenProviderSecret(federationcore.SealedSecret, int64) ([]byte, error)
}

type proofQueries interface {
	InvalidateVRChatOperatorSecret(context.Context, db.InvalidateVRChatOperatorSecretParams) (db.UpstreamIdp, error)
}

type Adapter struct {
	client       proofClient
	secrets      proofSecretStore
	kv           kv.Store
	queries      proofQueries
	publicOrigin string
	audit        audit.Writer
	now          func() time.Time
	random       io.Reader
}

func NewAdapter(client proofClient, secrets proofSecretStore, store kv.Store, queries proofQueries, publicOrigin string, writer audit.Writer) *Adapter {
	return &Adapter{client: client, secrets: secrets, kv: store, queries: queries, publicOrigin: strings.TrimSuffix(publicOrigin, "/"), audit: writer, now: time.Now, random: rand.Reader}
}

func (*Adapter) Protocol() string { return Protocol }

type adapterState struct {
	Step       string `json:"step"`
	UserID     string `json:"user_id,omitempty"`
	ProofToken string `json:"proof_token,omitempty"`
}

func (*Adapter) Begin(_ context.Context, _ federationcore.Provider, begin federationcore.BeginContext) (json.RawMessage, federationcore.NextAction, error) {
	if begin.Intent != federationcore.IntentEnroll && begin.Intent != federationcore.IntentLink {
		return nil, federationcore.NextAction{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	state, err := json.Marshal(adapterState{Step: stepIdentify})
	if err != nil {
		return nil, federationcore.NextAction{}, err
	}
	return state, federationcore.NextAction{Kind: federationcore.ActionCollectIdentity}, nil
}

func (a *Adapter) Advance(ctx context.Context, provider federationcore.Provider, raw json.RawMessage, input federationcore.ActionInput) (federationcore.AdvanceResult, error) {
	state, err := decodeAdapterState(raw)
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	switch state.Step {
	case stepIdentify:
		return a.collectIdentity(state, input)
	case stepProof:
		return a.publishProof(ctx, provider, state, input)
	default:
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
}

func decodeAdapterState(raw json.RawMessage) (adapterState, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state adapterState
	if err := decoder.Decode(&state); err != nil {
		return adapterState{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return adapterState{}, errors.New("vrchat: invalid adapter state")
	}
	return state, nil
}

func (a *Adapter) collectIdentity(state adapterState, input federationcore.ActionInput) (federationcore.AdvanceResult, error) {
	if input.Kind != federationcore.ActionCollectIdentity || state.UserID != "" || state.ProofToken != "" {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	userID, err := parseIdentity(input.Identity)
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureVRChatIdentityInvalid, nil)
	}
	proofURL, err := a.newProofURL()
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	state = adapterState{Step: stepProof, UserID: userID, ProofToken: strings.TrimPrefix(proofURL, a.publicOrigin+proofPathPrefix)}
	encoded, err := json.Marshal(state)
	if err != nil {
		return federationcore.AdvanceResult{}, err
	}
	next := &federationcore.NextAction{Kind: federationcore.ActionPublishProof, Public: map[string]any{"proofUrl": proofURL, "profileUrl": profileURLBase + userID}}
	return federationcore.AdvanceResult{Next: next, Candidate: &federationcore.IdentityKey{Issuer: issuerURL, Subject: userID}, State: encoded}, nil
}

func (a *Adapter) newProofURL() (string, error) {
	origin, err := url.Parse(a.publicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawPath != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return "", errors.New("vrchat: invalid public origin")
	}
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(a.random, bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	clear(bytes)
	return a.publicOrigin + proofPathPrefix + token, nil
}

func validProofToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	valid := err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
	clear(decoded)
	return valid
}

func (a *Adapter) publishProof(ctx context.Context, provider federationcore.Provider, state adapterState, input federationcore.ActionInput) (federationcore.AdvanceResult, error) {
	if input.Kind != federationcore.ActionPublishProof || !canonicalUserIDPattern.MatchString(state.UserID) || !validProofToken(state.ProofToken) {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureStateInvalid, nil)
	}
	if retry, err := a.providerBackoff(ctx, provider.ID); err != nil {
		return federationcore.AdvanceResult{}, err
	} else if retry > 0 {
		return federationcore.AdvanceResult{}, federationcore.NewRateLimitedFailure(retry)
	}
	if provider.Secret == nil || a.secrets == nil || a.client == nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureVRChatProviderNotReady, nil)
	}
	plaintext, err := a.secrets.OpenProviderSecret(*provider.Secret, provider.ID)
	if err != nil {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureVRChatProviderNotReady, nil)
	}
	defer clear(plaintext)
	cookies, err := a.client.DecodeCookies(plaintext, a.now())
	if err != nil || !usableOperatorAuthCookie(cookies, a.now()) {
		return federationcore.AdvanceResult{}, a.invalidateOperatorSnapshot(ctx, provider)
	}
	user, _, err := a.client.PublicUser(ctx, state.UserID, cookies)
	if err != nil {
		return federationcore.AdvanceResult{}, a.classifyUpstream(ctx, provider, err)
	}
	if user.ID != state.UserID || !canonicalUserIDPattern.MatchString(user.ID) {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureVRChatIdentityInvalid, nil)
	}
	matched := false
	for _, link := range user.BioLinks {
		if proofLinkMatches(link, a.publicOrigin, state.ProofToken) {
			matched = true
			break
		}
	}
	if !matched {
		return federationcore.AdvanceResult{}, federationcore.NewFailure(federationcore.FailureVRChatProofMissing, nil)
	}
	profileURL := profileURLBase + state.UserID
	identity := &federationcore.VerifiedIdentity{
		Issuer: issuerURL, Subject: state.UserID, DisplayName: user.DisplayName,
		EmailVerificationSupported: false, AMR: []string{"vrchat_profile"}, AvatarURL: user.CurrentAvatarThumbnailImageURL,
		UpstreamData: map[string]string{"userId": state.UserID, "displayName": user.DisplayName, "profileUrl": profileURL},
	}
	return federationcore.AdvanceResult{Identity: identity, Avatar: &federationcore.AvatarDelivery{URL: user.CurrentAvatarThumbnailImageURL}}, nil
}

func providerBackoffKey(providerID int64) string {
	return "federation:provider:" + strconv.FormatInt(providerID, 10) + ":backoff"
}

func (a *Adapter) providerBackoff(ctx context.Context, providerID int64) (time.Duration, error) {
	value, err := a.kv.Get(ctx, providerBackoffKey(providerID))
	if errors.Is(err, kv.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, federationcore.ErrKVUnavailable
	}
	deadline, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0, federationcore.ErrKVUnavailable
	}
	remaining := deadline.Sub(a.now())
	if remaining <= 0 {
		return 0, nil
	}
	if remaining > maxProviderBackoff {
		remaining = maxProviderBackoff
	}
	return remaining, nil
}

func (a *Adapter) invalidateOperatorSnapshot(ctx context.Context, provider federationcore.Provider) error {
	if a.queries != nil {
		_, err := a.queries.InvalidateVRChatOperatorSecret(ctx, db.InvalidateVRChatOperatorSecretParams{ID: provider.ID, Slug: provider.Slug, ExpectedSecretEnc: provider.Secret.Ciphertext, ExpectedSecretNonce: provider.Secret.Nonce, ExpectedKeyVersion: pgtype.Int4{Int32: provider.Secret.KeyVersion, Valid: true}})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return federationcore.NewFailure(federationcore.FailureUpstreamUnavailable, nil)
		}
		if err == nil {
			audit.RecordOrLog(ctx, a.audit, audit.Record{
				Factor: audit.FactorUpstreamIDP,
				Event:  "vrchat_operator_session_invalidated",
				Detail: map[string]any{
					"slug":     provider.Slug,
					"action":   "proof_lookup",
					"category": "authentication",
				},
			})
		}
	}
	return federationcore.NewFailure(federationcore.FailureVRChatProviderNotReady, nil)
}

func (a *Adapter) extendProviderBackoff(ctx context.Context, providerID int64, retry time.Duration) (time.Duration, error) {
	now := a.now()
	deadline := now.Add(retry).UTC()
	key := providerBackoffKey(providerID)
	encoded := deadline.Format(time.RFC3339Nano)
	for {
		currentRaw, err := a.kv.Get(ctx, key)
		if errors.Is(err, kv.ErrKeyNotFound) {
			stored, setErr := a.kv.SetNX(ctx, key, encoded, retry)
			if setErr != nil {
				return 0, federationcore.ErrKVUnavailable
			}
			if stored {
				return retry, nil
			}
			continue
		}
		if err != nil {
			return 0, federationcore.ErrKVUnavailable
		}
		current, err := time.Parse(time.RFC3339Nano, currentRaw)
		if err != nil {
			return 0, federationcore.ErrKVUnavailable
		}
		if !deadline.After(current) {
			remaining := current.Sub(now)
			if remaining > maxProviderBackoff {
				remaining = maxProviderBackoff
			}
			return remaining, nil
		}
		swapped, err := a.kv.CompareAndSwap(ctx, key, currentRaw, encoded, retry)
		if err != nil {
			return 0, federationcore.ErrKVUnavailable
		}
		if swapped {
			return retry, nil
		}
	}
}

func (a *Adapter) classifyUpstream(ctx context.Context, provider federationcore.Provider, upstream error) error {
	var mismatch *IdentityMismatchError
	if errors.As(upstream, &mismatch) {
		return federationcore.NewFailure(federationcore.FailureVRChatIdentityInvalid, nil)
	}
	var httpErr *HTTPError
	if errors.As(upstream, &httpErr) {
		switch {
		case httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden:
			return a.invalidateOperatorSnapshot(ctx, provider)
		case httpErr.Status == http.StatusNotFound:
			return federationcore.NewFailure(federationcore.FailureVRChatIdentityInvalid, nil)
		case httpErr.Status == http.StatusTooManyRequests:
			retry := httpErr.RetryAfter
			if retry <= 0 {
				retry = defaultRetryAfter
			}
			if retry > maxProviderBackoff {
				retry = maxProviderBackoff
			}
			storedRetry, err := a.extendProviderBackoff(ctx, provider.ID, retry)
			if err != nil {
				return err
			}
			return federationcore.NewRateLimitedFailure(storedRetry)
		}
	}
	return federationcore.NewFailure(federationcore.FailureUpstreamUnavailable, nil)
}

var _ federationcore.Adapter = (*Adapter)(nil)
