package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/weberr"
)

type forwardAuthApplication struct {
	Label   string `json:"label"`
	Host    string `json:"host,omitempty"`
	IconURL string `json:"iconUrl,omitempty"`
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
		app, ok, serr := p.samlContext(r.Context(), u)
		if serr != nil {
			fail("database_unavailable")
			return
		}
		if !ok {
			none()
			return
		}
		p.writeApplication(w, r.Context(), app.Label, app.Host, "saml_sp", strconv.FormatInt(app.SPID, 10))
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
		p.ordinaryOIDCLabel(w, r.Context(), client, params)
		return
	}
	if client.Disabled || !client.ForwardAuthEnabled || !client.ForwardAuthHost.Valid || client.ForwardAuthHost.String == "" ||
		params.Get("redirect_uri") != ForwardAuthCallbackURI(client.ForwardAuthHost.String) ||
		!slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		// A client that passes the authorize handler's gates but is not
		// forward-auth-bound is an ordinary OIDC request; the already-loaded
		// row answers it, keeping this path at one load too.
		p.ordinaryOIDCLabel(w, r.Context(), client, params)
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
	p.writeApplication(w, r.Context(), label, client.ForwardAuthHost.String, "oidc_client", client.ClientID)
}

// writeApplication answers with the application's label, host and icon. The
// icon is read only here, on an answer that already names the application, so
// it reveals nothing and null answers keep their read count.
func (p *Provider) writeApplication(w http.ResponseWriter, ctx context.Context, label, host, iconKind, iconID string) {
	app := &forwardAuthApplication{Label: label, Host: host}
	if p.iconURL != nil {
		app.IconURL = p.iconURL(ctx, iconKind, iconID)
	}
	_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{Application: app})
}

// ordinaryOIDCLabel answers for an /oauth/authorize return_to without a
// forward-auth binding, with the registered name of the client the caller
// loaded. A client or redirect_uri that fails the authorize checks is answered
// as no context rather than an error.
func (p *Provider) ordinaryOIDCLabel(w http.ResponseWriter, ctx context.Context, client db.OidcClient, params url.Values) {
	if client.Disabled || !slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{})
		return
	}
	// The redirect_uri was matched exactly against the registered list, so its
	// host is one the caller sent and reveals nothing new.
	var host string
	if requested, perr := url.Parse(params.Get("redirect_uri")); perr == nil {
		host = requested.Host
	}
	label := client.DisplayName
	if label == "" {
		label = host
	}
	if label == "" {
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{})
		return
	}
	p.writeApplication(w, ctx, label, host, "oidc_client", client.ClientID)
}
