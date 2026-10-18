package oidc

import (
	"net/http"
	"net/url"
)

// appAccessDenied renders the denied-access outcome for an authenticated user
// who is NOT authorized for a restricted client (RBAC). The user has a live,
// enabled session — this is an authorization denial, not an authentication one.
//
//   - Interactive flows are sent to the IdP's OWN /error page (a friendly
//     plain-language landing keyed on reason=app_access_denied + the client's
//     display name) — NOT to the RP, because a denied user should not be bounced
//     back into an RP error loop. returnTo is the address the flow was started
//     from (the authorize or require-identity URL): a denial can be lifted by an
//     administrator while the user waits, so the page offers to run that address
//     again. Pass "" when there is none to re-run.
//   - prompt=none flows (the RP forbade any interactive UI) instead receive the
//     protocol-native access_denied error redirected to the RP's redirect_uri,
//     per OIDC Core §3.1.2.6, so the RP can handle it programmatically.
func (p *Provider) appAccessDenied(w http.ResponseWriter, r *http.Request, redirectURI, appName, state, returnTo string, promptNone bool) {
	if promptNone {
		redirectError(w, r, redirectURI, errCodeAccessDenied, "not authorized for this application", state, p.cfg.OIDC.Issuer)
		return
	}
	u := p.cfg.OIDC.Issuer + "/error?reason=app_access_denied&app=" + url.QueryEscape(appName)
	if returnTo != "" {
		u += "&return_to=" + url.QueryEscape(returnTo)
	}
	http.Redirect(w, r, u, http.StatusFound)
}
