package saml

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
)

// LoginContextLabel resolves the application label the sign-in page shows for
// a return_to URL that points at this IdP's SAML endpoints: /saml/sso (the
// SP-initiated bounce carries the AuthnRequest in the query for the redirect
// binding — the POST binding re-POSTs the form, so its return_to is a POST
// target the GET context endpoint cannot replay) or /saml/sso/init.
//
// Like the OIDC forward-auth context this is a strictly read-only view over
// registered configuration: it re-runs the AuthnRequest parse (including the
// SP signature when required) without consuming the single-use request ID, and
// returns ok=false rather than an error for anything a user could have gotten
// wrong, so the page falls back to the plain sign-in instead of blocking it.
// err is non-nil only when storage failed, which the caller maps to 503.
func (i *IdP) LoginContextLabel(ctx context.Context, u *url.URL) (label string, ok bool, err error) {
	switch u.EscapedPath() {
	case "/saml/sso":
		req, perr := i.parseAuthnRequest(ctx, &http.Request{Method: http.MethodGet, URL: u})
		if perr != nil || req.SP.Disabled {
			return "", false, nil
		}
		return spLabel(req.SP), true, nil
	case "/saml/sso/init":
		spParam := u.Query().Get("sp")
		if spParam == "" {
			return "", false, nil
		}
		sp, gerr := i.queries.GetSAMLSPByEntityID(ctx, spParam)
		if gerr != nil {
			if errors.Is(gerr, pgx.ErrNoRows) {
				return "", false, nil
			}
			return "", false, gerr
		}
		if sp.Disabled || !sp.AllowIdpInitiated {
			return "", false, nil
		}
		return spLabel(sp), true, nil
	default:
		return "", false, nil
	}
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
