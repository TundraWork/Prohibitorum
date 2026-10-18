package oidc

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/weberr"
)

type forwardAuthApplication struct {
	Label string `json:"label"`
}
type forwardAuthLoginContext struct {
	Application *forwardAuthApplication `json:"application"`
}

// HandleForwardAuthLoginContext resolves the application label the sign-in
// page shows for a return_to that points back at this issuer. The
// forward-auth branch reads gateway state without consuming it or extending
// its lifetime; the ordinary-OIDC and SAML branches read registered
// configuration only. Only registered, bound application data reaches the
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
		u.Host == "" || u.User != nil || strings.Contains(outer.Get("return_to"), "#") {
		fail("bad_request")
		return
	}
	if u.EscapedPath() == "/saml/sso" || u.EscapedPath() == "/saml/sso/init" {
		if p.samlContext == nil {
			none()
			return
		}
		label, ok, serr := p.samlContext(r.Context(), u)
		if serr != nil {
			fail("database_unavailable")
			return
		}
		if !ok {
			none()
			return
		}
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{Application: &forwardAuthApplication{Label: label}})
		return
	}
	if u.EscapedPath() != "/oauth/authorize" {
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
	// Forward-auth needs state + PKCE; an ordinary OIDC request may omit
	// both. Either way the label comes from one client load below — the
	// count of database reads must not differ between an unknown and a
	// merely-not-forward-auth client_id, or the extra round trip would
	// reveal which client_ids exist.
	if params.Get("response_type") != "code" {
		none()
		return
	}
	ordinary := params.Get("state") == "" || params.Get("code_challenge") == "" || params.Get("code_challenge_method") != "S256"
	client, err := p.queries.GetOIDCClientAny(r.Context(), params.Get("client_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		none()
		return
	}
	if err != nil {
		fail("database_unavailable")
		return
	}
	if ordinary {
		p.ordinaryOIDCLabel(w, client, params)
		return
	}
	if client.Disabled || !client.ForwardAuthEnabled || !client.ForwardAuthHost.Valid || client.ForwardAuthHost.String == "" ||
		params.Get("redirect_uri") != ForwardAuthCallbackURI(client.ForwardAuthHost.String) ||
		!slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		// A client that passes the authorize handler's gates but is not
		// forward-auth-bound is an ordinary OIDC request; the already-loaded
		// row answers it, keeping this path at one load too.
		p.ordinaryOIDCLabel(w, client, params)
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

// ordinaryOIDCLabel answers for a return_to that is an /oauth/authorize URL
// without a forward-auth binding, using the client row the caller already
// loaded: the login bounce only happens after the authorize handler has
// validated the client and its redirect_uri, so the same checks here select
// the client's registered name; anything that fails them is answered as no
// context rather than an error.
func (p *Provider) ordinaryOIDCLabel(w http.ResponseWriter, client db.OidcClient, params url.Values) {
	if client.Disabled || !slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{})
		return
	}
	label := client.DisplayName
	if label == "" {
		// The request's own redirect_uri is exactly matched against the
		// registered list before this, so its host is a registered host the
		// caller already knows — the fallback never reveals an address the
		// caller did not send.
		if requested, perr := url.Parse(params.Get("redirect_uri")); perr == nil && requested.Host != "" {
			label = requested.Host
		}
	}
	if label == "" {
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{})
		return
	}
	_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{Application: &forwardAuthApplication{Label: label}})
}
