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

// LoginApplication is the registered application a sign-in bounce continues
// to: its label, the host the user returns to (empty when none is known), its
// uploaded icon URL (empty without one), and the owner kind and id the icon
// and registration are keyed by ("oidc_client" + client_id, or "saml_sp" + SP
// row id).
type LoginApplication struct {
	Label   string
	Host    string
	IconURL string
	Kind    string
	ID      string
}

// loginContextError carries the error code the login-context endpoint answers
// with: "bad_request" for a return_to it does not accept, or the store that
// failed.
type loginContextError struct{ code string }

func (e loginContextError) Error() string { return "login-context: " + e.code }

// IsLoginContextBadRequest reports whether a ResolveLoginApplication error is
// a return_to the resolver does not accept, as opposed to a store failure.
func IsLoginContextBadRequest(err error) bool {
	var lerr loginContextError
	return errors.As(err, &lerr) && lerr.code == "bad_request"
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
	if len(r.URL.RawQuery) > 16<<10 {
		fail("bad_request")
		return
	}
	outer, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(outer["return_to"]) != 1 || outer.Get("return_to") == "" {
		fail("bad_request")
		return
	}
	app, err := p.ResolveLoginApplication(r.Context(), outer.Get("return_to"))
	var lerr loginContextError
	if errors.As(err, &lerr) {
		fail(lerr.code)
		return
	}
	if err != nil {
		fail("server_error")
		return
	}
	if app == nil {
		_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{})
		return
	}
	_ = json.NewEncoder(w).Encode(forwardAuthLoginContext{Application: &forwardAuthApplication{
		Label: app.Label, Host: app.Host, IconURL: app.IconURL,
	}})
}

// ResolveLoginApplication names the registered application an absolute
// return_to on this issuer continues to: an /oauth/authorize request (ordinary
// OIDC or forward-auth) or a /saml/sso or /saml/sso/init bounce. It answers
// nil when the URL names no application it may show, and a loginContextError
// for a URL it does not accept or a store it cannot read. The count of reads
// is the same for an unknown and a known-but-unshowable application, so the
// answer's timing does not reveal which client_ids exist.
func (p *Provider) ResolveLoginApplication(ctx context.Context, returnTo string) (*LoginApplication, error) {
	badRequest := loginContextError{"bad_request"}
	u, err := url.Parse(returnTo)
	issuer, issuerErr := url.Parse(p.cfg.OIDC.Issuer)
	if err != nil || issuerErr != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host ||
		u.Host == "" || u.User != nil || strings.Contains(returnTo, "#") {
		return nil, badRequest
	}
	if u.EscapedPath() == "/saml/sso" || u.EscapedPath() == "/saml/sso/init" {
		if p.samlContext == nil {
			return nil, nil
		}
		app, ok, serr := p.samlContext(ctx, u)
		if serr != nil {
			return nil, loginContextError{"database_unavailable"}
		}
		if !ok {
			return nil, nil
		}
		return p.loginApplication(ctx, app.Label, app.Host, "saml_sp", strconv.FormatInt(app.SPID, 10)), nil
	}
	if u.EscapedPath() != "/oauth/authorize" {
		return nil, badRequest
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, badRequest
	}
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "state", "code_challenge", "code_challenge_method", "scope", "nonce"} {
		if len(params[key]) > 1 {
			return nil, badRequest
		}
	}
	for _, key := range []string{"client_id", "redirect_uri", "response_type"} {
		if params.Get(key) == "" {
			return nil, badRequest
		}
	}
	// Forward-auth needs state + PKCE; an ordinary OIDC request may omit
	// both. Either way the label comes from one client load below — the
	// count of database reads must not differ between an unknown and a
	// merely-not-forward-auth client_id, or the extra round trip would
	// reveal which client_ids exist.
	if params.Get("response_type") != "code" {
		return nil, nil
	}
	ordinary := params.Get("state") == "" || params.Get("code_challenge") == "" || params.Get("code_challenge_method") != "S256"
	client, err := p.queries.GetOIDCClientAny(ctx, params.Get("client_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, loginContextError{"database_unavailable"}
	}
	if ordinary {
		return p.ordinaryOIDCApplication(ctx, client, params), nil
	}
	if client.Disabled || !client.ForwardAuthEnabled || !client.ForwardAuthHost.Valid || client.ForwardAuthHost.String == "" ||
		params.Get("redirect_uri") != ForwardAuthCallbackURI(client.ForwardAuthHost.String) ||
		!slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		// A client that passes the authorize handler's gates but is not
		// forward-auth-bound is an ordinary OIDC request; the already-loaded
		// row answers it, keeping this path at one load too.
		return p.ordinaryOIDCApplication(ctx, client, params), nil
	}
	raw, err := p.kv.Get(ctx, faStateKey(params.Get("state")))
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, loginContextError{"kv_unavailable"}
	}
	var state faState
	if json.Unmarshal([]byte(raw), &state) != nil || state.ClientID != client.ClientID || state.Verifier == "" ||
		!verifyPKCE(state.Verifier, params.Get("code_challenge")) {
		return nil, nil
	}
	original, err := url.Parse(state.OriginalURL)
	if err != nil || original.Host != client.ForwardAuthHost.String || original.User != nil ||
		(original.Scheme != "https" && original.Scheme != "http") {
		return nil, nil
	}
	label := client.DisplayName
	if label == "" {
		label = client.ForwardAuthHost.String
	}
	return p.loginApplication(ctx, label, client.ForwardAuthHost.String, "oidc_client", client.ClientID), nil
}

// loginApplication completes an application that already has a name with its
// icon. The icon is read only here, on an answer that already names the
// application, so it reveals nothing and null answers keep their read count.
func (p *Provider) loginApplication(ctx context.Context, label, host, kind, id string) *LoginApplication {
	app := &LoginApplication{Label: label, Host: host, Kind: kind, ID: id}
	if p.iconURL != nil {
		app.IconURL = p.iconURL(ctx, kind, id)
	}
	return app
}

// ordinaryOIDCApplication answers for an /oauth/authorize return_to without a
// forward-auth binding, with the registered name of the client the caller
// loaded. A client or redirect_uri that fails the authorize checks names no
// application.
func (p *Provider) ordinaryOIDCApplication(ctx context.Context, client db.OidcClient, params url.Values) *LoginApplication {
	if client.Disabled || !slices.Contains(client.RedirectUris, params.Get("redirect_uri")) {
		return nil
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
		return nil
	}
	return p.loginApplication(ctx, label, host, "oidc_client", client.ClientID)
}
