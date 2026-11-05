package saml

import (
	"net/http"
	"net/url"
	"strings"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/session"
)

// accountChoiceParam carries an account chooser ticket back to an SSO
// endpoint. Like reauth it is appended after the SP-signed query string, which
// parseAuthnRequest ignores.
const accountChoiceParam = "account_choice"

// chooseSession settles which signed-in account an SSO request acts as
// (design 5.4): a valid account choice for this flow and SP naming an account
// signed in here wins; otherwise a browser with one account (or none) keeps
// the default session. needChoice reports a browser holding several accounts
// with no valid choice, which the caller sends to the account chooser.
func (i *IdP) chooseSession(r *http.Request, kind, app string) (sess *authn.Session, needChoice bool) {
	ctx := r.Context()
	b := session.BrowserFromContext(ctx)
	if b == nil {
		return authn.SessionFromContext(ctx), false
	}
	if id := authn.ChosenAccount(ctx, i.kv, r.URL.Query().Get(accountChoiceParam), kind, app); id != 0 {
		if s := b.ForAccount(id); s != nil {
			return s, false
		}
	}
	if b.Count() > 1 || b.Unsettled() {
		return nil, true
	}
	return authn.SessionFromContext(ctx), false
}

// redirectToChooser sends the reader to the account chooser for this request.
// resumePath is the same-origin address the choice comes back to; any stale
// account_choice or reauth on it is dropped.
func (i *IdP) redirectToChooser(w http.ResponseWriter, r *http.Request, kind, app, resumePath string) error {
	nonce, err := authn.CreateAccountChooser(r.Context(), i.kv, authn.AccountChooserTicket{
		Kind:      kind,
		App:       app,
		ResumeURL: withoutQueryParams(resumePath, accountChoiceParam, "reauth"),
	})
	if err != nil {
		return err
	}
	http.Redirect(w, r, i.baseURL()+"/choose-account?ticket="+url.QueryEscape(nonce), http.StatusFound)
	return nil
}

// withoutQueryParams drops the named parameters from a path's raw query,
// leaving every other octet as it was: the redirect binding's signature covers
// the raw SAMLRequest/RelayState/SigAlg bytes, so re-encoding would break it.
func withoutQueryParams(path string, names ...string) string {
	base, rawQuery, ok := strings.Cut(path, "?")
	if !ok {
		return path
	}
	kept := rawQueryWithout(rawQuery, names...)
	if len(kept) == 0 {
		return base
	}
	return base + "?" + strings.Join(kept, "&")
}

func rawQueryWithout(rawQuery string, names ...string) []string {
	kept := make([]string, 0)
	for _, p := range strings.Split(rawQuery, "&") {
		if p == "" {
			continue
		}
		drop := false
		for _, n := range names {
			if p == n || strings.HasPrefix(p, n+"=") {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, p)
		}
	}
	return kept
}

// withAccountRef adds the `u` of accountID to a console page address when the
// browser holds more than one account, so the page acts as that account
// rather than the default one.
func (i *IdP) withAccountRef(r *http.Request, target string, accountID int32) string {
	b := session.BrowserFromContext(r.Context())
	if i.refs == nil || b == nil || b.Count() <= 1 {
		return target
	}
	u := i.refs.Shorts(b.AccountIDs())[accountID]
	if u == "" {
		return target
	}
	return target + "&u=" + url.QueryEscape(u)
}
