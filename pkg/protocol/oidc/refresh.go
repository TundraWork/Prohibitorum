package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// RefreshTokenTTL is the DEFAULT lifetime of a refresh token (and its family
// record) in KV, used when oidc.refresh_token_ttl is unset. 30 days is a
// conventional refresh lifetime; rotation slides this window forward on each
// successful exchange so an actively-used family persists, while an abandoned
// one expires. Callers pass the effective TTL (p.refreshTokenTTL()) into
// issueRefresh / rotateRefresh; never read this const directly at a store site.
const RefreshTokenTTL = 30 * 24 * time.Hour

// errRefreshInvalid is returned when a presented refresh token does not resolve
// to a live family — it never existed, expired, or the family was revoked
// (explicitly or by a reuse-detection trip). Callers map this to the OAuth
// invalid_grant error.
var errRefreshInvalid = errors.New("oidc: refresh token invalid")

// errRefreshReuse is returned when a superseded (already-rotated) refresh token
// is presented. This is the reuse-detection trip: the entire family is revoked
// as a side effect, defeating a stolen-token replay (OAuth 2.0 Security BCP
// §4.13.2). Callers map this to invalid_grant.
var errRefreshReuse = errors.New("oidc: refresh token reuse detected")

// errRotationInProgress is returned when a concurrent rotation holds the lock
// for the presented token. It is BENIGN (not reuse): the caller maps it to a
// retryable invalid_grant and does NOT revoke the family.
var errRotationInProgress = errors.New("oidc: refresh rotation in progress")

// refreshIdempotencyWindow bounds how long a just-rotated (previous) refresh
// token may be re-presented to receive the SAME successor instead of tripping
// reuse detection. Covers benign client double-submit / network retry. Kept
// short: within this window a stolen previous token could also redeem the
// successor, an accepted tradeoff (the token is already compromised in that
// case, and false family revocation is user-hostile).
const refreshIdempotencyWindow = 10 * time.Second

// refreshLockKey is the SetNX rotation-lock key for a presented token.
func refreshLockKey(token string) string { return "oidc:refresh:lock:" + token }

// refreshFamily is the per-family record for a chain of rotated refresh tokens.
// CurrentToken names the single token that may be exchanged; every prior token
// in the chain resolves to this family but, being != CurrentToken, trips reuse
// detection if presented. The remaining fields are the authentication snapshot
// carried forward into each refreshed access/ID token.
type refreshFamily struct {
	FamilyID           string    `json:"family_id"`
	CurrentToken       string    `json:"current_token"`
	ClientID           string    `json:"client_id"`
	AccountID          int32     `json:"account_id"`
	SessionID          string    `json:"session_id"`
	Scope              []string  `json:"scope"`
	AuthTime           time.Time `json:"auth_time"`
	AMR                []string  `json:"amr"`
	ACR                string    `json:"acr"`
	IssuedAt           time.Time `json:"issued_at"`
	PreviousToken      string    `json:"previous_token,omitempty"`
	PreviousValidUntil time.Time `json:"previous_valid_until"`
}

// refreshFamilyKey is the KV key under which a family record is stored.
func refreshFamilyKey(fid string) string { return "oidc:refresh:family:" + fid }

// refreshTokenKey is the KV key under which a token→family-id mapping is stored.
func refreshTokenKey(token string) string { return "oidc:refresh:" + token }

// randToken returns 32 bytes of cryptographic randomness, base64url-encoded
// without padding so it is URL-safe. Used for both opaque refresh tokens and
// family identifiers. (Kept local to refresh.go to keep this change scoped;
// codes.go inlines the equivalent.)
func randToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// putFamily writes the family record (JSON) and a token→family-id mapping for
// fam.CurrentToken, both with the given ttl (the caller passes the effective
// p.refreshTokenTTL()). It is used both to seed a new family and to extend
// (slide) an existing one on rotation.
func putFamily(ctx context.Context, store kv.Store, fam *refreshFamily, ttl time.Duration) error {
	payload, err := json.Marshal(fam)
	if err != nil {
		return fmt.Errorf("oidc: marshal refresh family: %w", err)
	}
	if err := store.SetEx(ctx, refreshFamilyKey(fam.FamilyID), string(payload), ttl); err != nil {
		return err
	}
	if err := store.SetEx(ctx, refreshTokenKey(fam.CurrentToken), fam.FamilyID, ttl); err != nil {
		return err
	}
	return nil
}

// loadFamily resolves a token→family mapping and loads the named family record.
// A miss on either lookup (token unknown/expired, or family revoked/expired)
// returns errRefreshInvalid; a malformed family payload is a wrapped decode
// error, distinct from a miss, since it signals corruption rather than reuse.
func loadFamily(ctx context.Context, store kv.Store, presented string) (*refreshFamily, error) {
	fid, err := store.Get(ctx, refreshTokenKey(presented))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}
	raw, err := store.Get(ctx, refreshFamilyKey(fid))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}
	var fam refreshFamily
	if err := json.Unmarshal([]byte(raw), &fam); err != nil {
		return nil, fmt.Errorf("oidc: unmarshal refresh family: %w", err)
	}
	return &fam, nil
}

// issueRefresh seeds a NEW refresh-token family. It takes fam by value,
// generates a random FamilyID and an opaque CurrentToken, stamps IssuedAt, and
// writes both the family record (oidc:refresh:family:<fid>) and the
// token→family mapping (oidc:refresh:<token>) with TTL RefreshTokenTTL. It
// returns the freshly minted token and the generated family id. The caller need
// not pre-populate FamilyID/CurrentToken/IssuedAt; they are set here.
func issueRefresh(ctx context.Context, store kv.Store, fam refreshFamily, ttl time.Duration) (token string, familyID string, err error) {
	fid, err := randToken()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate refresh family id: %w", err)
	}
	token, err = randToken()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate refresh token: %w", err)
	}
	fam.FamilyID = fid
	fam.CurrentToken = token
	fam.IssuedAt = time.Now().UTC()

	if err := putFamily(ctx, store, &fam, ttl); err != nil {
		return "", "", err
	}
	return token, fid, nil
}

// rotateRefresh performs a single-use exchange of a refresh token, made
// atomic by a per-token SetNX rotation lock and made benign-concurrency-safe
// by a previous-token idempotency window on the family record.
//
//   - Acquire the rotation lock (SetNX). If held, a concurrent rotation for
//     this exact token is in flight → errRotationInProgress (retryable, no
//     revoke).
//   - Resolve the family; a miss → errRefreshInvalid.
//   - presented == PreviousToken and within PreviousValidUntil → benign
//     idempotent replay: return the already-rotated CurrentToken (caller
//     re-mints access/ID tokens). No second mint.
//   - presented != CurrentToken (and not valid-previous) → superseded/stolen
//     → revoke family, errRefreshReuse.
//   - presented == CurrentToken → mint newToken, record PreviousToken=presented,
//     PreviousValidUntil, set CurrentToken=newToken, persist.
//
// rotated reports whether this call performed a real rotation (true) vs served
// an idempotent replay (false) — the caller uses it to pick the audit reason.
func rotateRefresh(ctx context.Context, store kv.Store, presented string, ttl time.Duration) (fam *refreshFamily, newToken string, rotated bool, err error) {
	got, lockErr := store.SetNX(ctx, refreshLockKey(presented), "1", refreshIdempotencyWindow)
	if lockErr != nil {
		return nil, "", false, lockErr // fail closed: no rotation, no revoke
	}
	if !got {
		return nil, "", false, errRotationInProgress
	}
	defer func() { _ = store.Del(ctx, refreshLockKey(presented)) }()

	fam, err = loadFamily(ctx, store, presented)
	if err != nil {
		return nil, "", false, err
	}

	now := time.Now().UTC()
	if presented == fam.PreviousToken && now.Before(fam.PreviousValidUntil) {
		return fam, fam.CurrentToken, false, nil
	}

	if presented != fam.CurrentToken {
		if delErr := store.Del(ctx, refreshFamilyKey(fam.FamilyID)); delErr != nil {
			return nil, "", false, delErr
		}
		return nil, "", false, errRefreshReuse
	}

	minted, err := randToken()
	if err != nil {
		return nil, "", false, fmt.Errorf("oidc: generate refresh token: %w", err)
	}
	fam.PreviousToken = presented
	fam.PreviousValidUntil = now.Add(refreshIdempotencyWindow)
	fam.CurrentToken = minted
	if err := putFamily(ctx, store, fam, ttl); err != nil {
		return nil, "", false, err
	}
	return fam, minted, true, nil
}

// lookupRefresh resolves a presented token to its family WITHOUT mutating
// anything — no rotation, no revocation, no TTL change. It returns
// (family, true) when both the token mapping and family record resolve, else
// (nil, false). Used by /introspect and /revoke, which must not consume the
// token. Note this does not check presented == CurrentToken; a superseded
// token still resolves to its (live) family here.
func lookupRefresh(ctx context.Context, store kv.Store, presented string) (*refreshFamily, bool) {
	fam, err := loadFamily(ctx, store, presented)
	if err != nil {
		return nil, false
	}
	return fam, true
}

// revokeFamily deletes a family record by id, invalidating every token in the
// chain (subsequent rotate/lookup of any of them resolves the token mapping but
// misses the family → errRefreshInvalid / false). Deleting an absent family is
// a no-op.
func revokeFamily(ctx context.Context, store kv.Store, familyID string) error {
	return store.Del(ctx, refreshFamilyKey(familyID))
}

// grantRefreshToken implements the RFC 6749 §6 refresh_token grant. It rotates
// the presented token (single-use; reuse trips family revocation), or — for a
// just-rotated token re-presented within the idempotency window — serves the
// same successor without a second mint. Either way it re-checks the bound
// client and account and re-issues a fresh access + ID token plus the
// (rotated or replayed) refresh token. The re-issued ID token carries the
// family's snapshotted auth_time/amr/acr/sid and omits nonce (none is
// snapshotted).
func (p *Provider) grantRefreshToken(w http.ResponseWriter, r *http.Request, client db.OidcClient) {
	ctx := r.Context()
	presented := r.PostForm.Get("refresh_token")

	fam, newToken, rotated, err := rotateRefresh(ctx, p.kv, presented, p.refreshTokenTTL())
	if errors.Is(err, errRotationInProgress) {
		p.auditTokenEvent(ctx, r, audit.EventFail, nil, map[string]any{
			"reason":    "refresh_rotation_in_progress",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "refresh rotation in progress, retry")
		return
	}
	if errors.Is(err, errRefreshReuse) {
		p.auditTokenEvent(ctx, r, audit.EventFail, nil, map[string]any{
			"reason":    "refresh_reuse",
			"client_id": client.ClientID,
		})
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "refresh token reuse detected")
		return
	}
	if err != nil {
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "invalid refresh token")
		return
	}

	// Client binding (RFC 6749 §6): a refresh token presented by a client other
	// than the one it was issued to is anomalous. The family is live (rotated or
	// served an idempotent replay) — revoke the whole family (treat the mismatch
	// as an attack) before refusing.
	if fam.ClientID != client.ClientID {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "client mismatch")
		return
	}

	// Re-check the account on every refresh: a disabled account's family must
	// die so the long-lived grant cannot outlive the account's standing.
	acct, err := p.queries.GetAccountByID(ctx, fam.AccountID)
	if err != nil {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "account not found")
		return
	}
	if acct.Disabled {
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "account disabled")
		return
	}

	now := time.Now()
	accessToken, idToken, err := p.mintAccessAndIDTokens(ctx, acct, client.ClientID, "" /*nonce*/, fam.SessionID, fam.ACR, fam.AMR, fam.Scope, fam.AuthTime, now)
	if err != nil {
		// The family is live (from this or a prior rotation). Returning no token
		// would leave it in a live-but-unusable state the client is locked out of.
		// Fail closed: revoke the family so the client cleanly re-authenticates
		// rather than wedging.
		_ = revokeFamily(ctx, p.kv, fam.FamilyID)
		writeOIDCError(w, http.StatusInternalServerError, errCodeServerError, "could not mint tokens")
		return
	}

	reason := "refresh_rotated"
	if !rotated {
		reason = "refresh_idempotent_replay"
	}
	acctID := acct.ID
	p.auditTokenEvent(ctx, r, audit.EventUse, &acctID, map[string]any{
		"reason":    reason,
		"client_id": client.ClientID,
	})

	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(p.accessTokenTTL().Seconds()),
		IDToken:      idToken,
		RefreshToken: newToken,
		Scope:        strings.Join(fam.Scope, " "),
	})
}
