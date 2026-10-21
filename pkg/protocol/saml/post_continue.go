package saml

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"prohibitorum/pkg/kv"
)

// An HTTP-POST AuthnRequest travels in the request body, which a browser
// cannot carry through the sign-in page. When such a request has to wait for
// a sign-in, the form the SP posted is kept for a short while under a random
// key, and the sign-in returns to GET /saml/sso?post=<key>, which replays it
// as the same POST. Every check then runs again exactly as it did the first
// time — the SP lookup, the ACS match, the enveloped signature, the
// IssueInstant window — and the single-use request ID is still spent only
// when the assertion is issued.
//
// The kept form is only what the SP handed the browser, so the key reveals
// nothing the browser did not already hold. Anyone can post a form before
// signing in, so what is kept is bounded: a real AuthnRequest is a few KiB,
// and a larger one is refused rather than kept; the same form posted again
// reuses the entry it already has. The form is kept for twice the IssueInstant
// window, so a request is refused for its age before its form expires; once
// the request is spent its key is dropped, and a used link always ends the
// same way.

const (
	postContinueParam  = "post"
	postContinuePrefix = "saml:post-continue:"
	postContinueTTL    = 2 * AuthnRequestTTL
	// maxKeptSAMLRequest bounds the base64 SAMLRequest kept for a sign-in,
	// and maxKeptRelayState its RelayState (the spec allows 80 bytes).
	maxKeptSAMLRequest = 32 << 10
	maxKeptRelayState  = 1 << 10
)

// ErrKeptRequestExpired is a ?post= key with no kept form behind it: the
// sign-in took longer than the form is kept, or the key was never issued.
var ErrKeptRequestExpired = errors.New("saml: the kept POST request has expired")

// postContinueForm is the part of an HTTP-POST AuthnRequest the IdP reads.
type postContinueForm struct {
	SAMLRequest string `json:"saml_request"`
	RelayState  string `json:"relay_state,omitempty"`
}

// keepPostRequest stores the form of a POST-binding request and returns the
// key the sign-in comes back with.
// A form too large to be a real request is ErrOversizeRequest.
func (i *IdP) keepPostRequest(ctx context.Context, r *http.Request) (string, error) {
	form := postContinueForm{
		SAMLRequest: r.PostFormValue("SAMLRequest"),
		RelayState:  r.PostFormValue("RelayState"),
	}
	if len(form.SAMLRequest) > maxKeptSAMLRequest || len(form.RelayState) > maxKeptRelayState {
		return "", fmt.Errorf("%w: a %d-byte request is too large to keep through a sign-in", ErrOversizeRequest, len(form.SAMLRequest))
	}
	raw, err := json.Marshal(form)
	if err != nil {
		return "", err
	}
	// The same form posted again — a reload, or one captured request posted
	// over and over — reuses its entry. The whole form is the index, RelayState
	// included: the POST binding does not sign RelayState, so two posts of one
	// request with different RelayStates are two different forms.
	digest := sha256.Sum256(raw)
	index := postContinuePrefix + "form:" + hex.EncodeToString(digest[:])
	if existing, gerr := i.kv.Get(ctx, index); gerr == nil {
		if _, serr := i.kv.Get(ctx, postContinuePrefix+existing); serr == nil {
			return existing, nil
		}
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(buf[:])
	if err := i.kv.SetEx(ctx, postContinuePrefix+key, string(raw), postContinueTTL); err != nil {
		return "", err
	}
	if err := i.kv.SetEx(ctx, index, key, postContinueTTL); err != nil {
		return "", err
	}
	return key, nil
}

// consumeRequest spends the request's single-use ID and, for a request
// replayed from its kept form, drops the form: a used link always ends on
// the expired-request page rather than the replay check.
func (i *IdP) consumeRequest(ctx context.Context, r *http.Request, entityID, requestID string) error {
	if err := i.consumeAuthnRequestID(ctx, entityID, requestID); err != nil {
		return err
	}
	if key := r.URL.Query().Get(postContinueParam); key != "" && r.Method == http.MethodPost {
		_ = i.kv.Del(ctx, postContinuePrefix+key)
	}
	return nil
}

// postContinueRequest turns GET /saml/sso?post=<key> back into the POST the
// SP sent, carrying the rest of the query (a reauth nonce) along. ok is false
// when the URL carries no key. A key that has expired or was never issued is
// ErrKeptRequestExpired, which tells the reader the sign-in took too long.
func (i *IdP) postContinueRequest(ctx context.Context, r *http.Request) (*http.Request, bool, error) {
	key := r.URL.Query().Get(postContinueParam)
	if r.Method != http.MethodGet || key == "" {
		return nil, false, nil
	}
	raw, err := i.kv.Get(ctx, postContinuePrefix+key)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, true, ErrKeptRequestExpired
	}
	if err != nil {
		return nil, true, err
	}
	var form postContinueForm
	if err := json.Unmarshal([]byte(raw), &form); err != nil || form.SAMLRequest == "" {
		return nil, true, ErrKeptRequestExpired
	}
	values := url.Values{"SAMLRequest": {form.SAMLRequest}}
	if form.RelayState != "" {
		values.Set("RelayState", form.RelayState)
	}
	replay := r.Clone(ctx)
	replay.Method = http.MethodPost
	replay.Body = http.NoBody
	replay.ContentLength = 0
	replay.PostForm = values
	replay.Form = values
	return replay, true, nil
}

// ssoReturnURL is the address a sign-in returns to for this SSO request: the
// same URL for the redirect binding and for a request already replayed from
// its key, and the kept form's key for a POST that came from the SP.
func (i *IdP) ssoReturnURL(ctx context.Context, r *http.Request) (string, error) {
	if r.Method != http.MethodPost || r.URL.Query().Get(postContinueParam) != "" {
		return i.baseURL() + r.URL.RequestURI(), nil
	}
	key, err := i.keepPostRequest(ctx, r)
	if err != nil {
		return "", err
	}
	return i.baseURL() + "/saml/sso?" + postContinueParam + "=" + key, nil
}
