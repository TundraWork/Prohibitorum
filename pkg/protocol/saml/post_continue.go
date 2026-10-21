package saml

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
// nothing the browser did not already hold. A request outlives its
// IssueInstant window by at most the kept form's lifetime, after which it
// would be refused anyway.

const (
	postContinueParam  = "post"
	postContinuePrefix = "saml:post-continue:"
	postContinueTTL    = 2 * AuthnRequestTTL
)

// postContinueForm is the part of an HTTP-POST AuthnRequest the IdP reads.
type postContinueForm struct {
	SAMLRequest string `json:"saml_request"`
	RelayState  string `json:"relay_state,omitempty"`
}

// keepPostRequest stores the form of a POST-binding request and returns the
// key the sign-in comes back with.
func (i *IdP) keepPostRequest(ctx context.Context, r *http.Request) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(buf[:])
	raw, err := json.Marshal(postContinueForm{
		SAMLRequest: r.PostFormValue("SAMLRequest"),
		RelayState:  r.PostFormValue("RelayState"),
	})
	if err != nil {
		return "", err
	}
	if err := i.kv.SetEx(ctx, postContinuePrefix+key, string(raw), postContinueTTL); err != nil {
		return "", err
	}
	return key, nil
}

// postContinueRequest turns GET /saml/sso?post=<key> back into the POST the
// SP sent, carrying the rest of the query (a reauth nonce) along. ok is false
// when the URL carries no key. A key that has expired or was never issued is
// ErrMissingSAMLRequest, so it ends on the same page as any other request
// the IdP cannot read.
func (i *IdP) postContinueRequest(ctx context.Context, r *http.Request) (*http.Request, bool, error) {
	key := r.URL.Query().Get(postContinueParam)
	if r.Method != http.MethodGet || key == "" {
		return nil, false, nil
	}
	raw, err := i.kv.Get(ctx, postContinuePrefix+key)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, true, fmt.Errorf("%w: the kept POST request has expired", ErrMissingSAMLRequest)
	}
	if err != nil {
		return nil, true, err
	}
	var form postContinueForm
	if err := json.Unmarshal([]byte(raw), &form); err != nil || form.SAMLRequest == "" {
		return nil, true, fmt.Errorf("%w: the kept POST request is unreadable", ErrMissingSAMLRequest)
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
