package oidc

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/weberr"
)

type forwardAuthApplication struct {
	Label string `json:"label"`
}
type forwardAuthLoginContext struct {
	Application *forwardAuthApplication `json:"application"`
}

// HandleForwardAuthLoginContext reads gateway state without consuming it or
// extending its lifetime. Only registered, bound application data reaches the
// login page; this endpoint never authorizes a request.
func (p *Provider) HandleForwardAuthLoginContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	fail := func(code string) { weberr.WriteJSON(w, code, nil, weberr.RequestIDFromContext(r.Context())) }
	none := func() { _ = json.NewEncoder(w).Encode(forwardAuthLoginContext{}) }
	if len(r.URL.RawQuery) > 16<<10 {
		fail("bad_request")
		return
	}
	outer, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(outer["return_to"]) != 1 || outer.Get("return_to") == "" {
		fail("bad_request")
		return
	}
	u, err := url.Parse(outer.Get("return_to"))
	issuer, issuerErr := url.Parse(p.cfg.OIDC.Issuer)
	if err != nil || issuerErr != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host ||
		u.Host == "" || u.User != nil || strings.Contains(outer.Get("return_to"), "#") || u.EscapedPath() != "/oauth/authorize" {
		fail("bad_request")
		return
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		fail("bad_request")
		return
	}
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "state", "code_challenge", "code_challenge_method", "scope", "nonce"} {
		if len(params[key]) > 1 {
			fail("bad_request")
			return
		}
	}
	for _, key := range []string{"client_id", "redirect_uri", "response_type"} {
		if params.Get(key) == "" {
			fail("bad_request")
			return
		}
	}
	// State and PKCE are optional for ordinary OIDC requests.
	if params.Get("response_type") != "code" || params.Get("state") == "" || params.Get("code_challenge") == "" || params.Get("code_challenge_method") != "S256" {
		none()
		return
	}
	client, err := p.queries.GetOIDCClientAny(r.Context(), params.Get("client_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		none()
		return
	}
	if err != nil {
		fail("database_unavailable")
		return
	}
	if client.Disabled || !client.ForwardAuthEnabled || !client.ForwardAuthHost.Valid || client.ForwardAuthHost.String == "" ||
		params.Get("redirect_uri") != ForwardAuthCallbackURI(client.ForwardAuthHost.String) ||
		!slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		none()
		return
	}
	raw, err := p.kv.Get(r.Context(), faStateKey(params.Get("state")))
	if errors.Is(err, kv.ErrKeyNotFound) {
		none()
		return
	}
	if err != nil {
		fail("kv_unavailable")
		return
	}
	var state faState
	if json.Unmarshal([]byte(raw), &state) != nil || state.ClientID != client.ClientID || state.Verifier == "" ||
		!verifyPKCE(state.Verifier, params.Get("code_challenge")) {
		none()
		return
	}
	original, err := url.Parse(state.OriginalURL)
	if err != nil || original.Host != client.ForwardAuthHost.String || original.User != nil ||
		(original.Scheme != "https" && original.Scheme != "http") {
		none()
		return
	}
	label := client.DisplayName
	if label == "" {
		label = client.ForwardAuthHost.String
	}
	_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{Application: &forwardAuthApplication{Label: label}})
}
