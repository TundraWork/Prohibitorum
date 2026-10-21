package saml

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
)

// LoginApplication is what the sign-in page shows about a SAML application:
// its label, the host its assertion would be posted to (empty when none is
// derivable), and the SP row id its uploaded icon is keyed by.
type LoginApplication struct {
	Label string
	Host  string
	SPID  int64
}

// LoginContext resolves the application the sign-in page shows for a
// return_to URL that points at this IdP's SAML endpoints: /saml/sso (the
// SP-initiated bounce carries the AuthnRequest in the query for the redirect
// binding, and the key of its kept form for the POST binding) or
// /saml/sso/init.
//
// Like the OIDC forward-auth context this is a strictly read-only view over
// registered configuration: it re-runs the AuthnRequest parse (including the
// SP signature when required) without consuming the single-use request ID, and
// returns ok=false rather than an error for anything a user could have gotten
// wrong, so the page falls back to the plain sign-in instead of blocking it.
// err is non-nil only when storage failed, which the caller maps to 503.
func (i *IdP) LoginContext(ctx context.Context, u *url.URL) (app LoginApplication, ok bool, err error) {
	switch u.EscapedPath() {
	case "/saml/sso":
		request := &http.Request{Method: http.MethodGet, URL: u}
		// A POST-binding request waiting on the sign-in is named from its
		// kept form, read without being spent.
		if replay, isKept, kerr := i.postContinueRequest(ctx, request.WithContext(ctx)); isKept {
			if kerr != nil {
				if errors.Is(kerr, ErrMissingSAMLRequest) {
					return LoginApplication{}, false, nil
				}
				return LoginApplication{}, false, kerr
			}
			request = replay
		}
		req, perr := i.parseAuthnRequest(ctx, request)
		if perr != nil || req.SP.Disabled {
			return LoginApplication{}, false, nil
		}
		// req.ACSURL already passed the registered-endpoint match.
		return LoginApplication{Label: spLabel(req.SP), Host: urlHost(req.ACSURL), SPID: req.SP.ID}, true, nil
	case "/saml/sso/init":
		spParam := u.Query().Get("sp")
		if spParam == "" {
			return LoginApplication{}, false, nil
		}
		sp, gerr := i.queries.GetSAMLSPByEntityID(ctx, spParam)
		if gerr != nil {
			if errors.Is(gerr, pgx.ErrNoRows) {
				return LoginApplication{}, false, nil
			}
			return LoginApplication{}, false, gerr
		}
		if sp.Disabled || !sp.AllowIdpInitiated {
			return LoginApplication{}, false, nil
		}
		// IdP-initiated SSO posts to the SP's default ACS; a failed lookup only
		// costs the host.
		acsURL, _ := i.resolveACS(ctx, sp.ID, "", "")
		return LoginApplication{Label: spLabel(sp), Host: urlHost(acsURL), SPID: sp.ID}, true, nil
	default:
		return LoginApplication{}, false, nil
	}
}

// urlHost is the host (with port) of raw, or "" when it does not parse.
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// spLabel is the display name, or the SP's entity_id when no name is set —
// the same empty-name fallback the forward-auth context applies with its
// registered host.
func spLabel(sp db.SamlSp) string {
	if sp.DisplayName != "" {
		return sp.DisplayName
	}
	return sp.EntityID
}
