package server

import (
	"context"
	"strconv"
	"strings"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
)

// loginAppQueries is the narrow DB surface loginApplicationView reads. Tests
// inject a fake via Server.loginAppOverride; production uses s.queries.
type loginAppQueries interface {
	GetOIDCClientAny(ctx context.Context, clientID string) (db.OidcClient, error)
	GetSAMLSPByID(ctx context.Context, id int64) (db.SamlSp, error)
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

func (s *Server) loginAppQ() loginAppQueries {
	if s.loginAppOverride != nil {
		return s.loginAppOverride
	}
	if s.queries == nil {
		return nil
	}
	return s.queries
}

// federationAppRef names the application a federation flow's return_to (a
// path on this origin) continues to, by the same rules as the login-context
// endpoint. Anything it cannot name, including a store failure, is a flow
// without an application: the sign-in itself never depends on it.
func (s *Server) federationAppRef(ctx context.Context, returnTo string) *federation.AppRef {
	if s.oidcOP == nil || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		return nil
	}
	app, err := s.oidcOP.ResolveLoginApplication(ctx, s.config.OIDC.Issuer+returnTo)
	if err != nil || app == nil {
		return nil
	}
	return &federation.AppRef{Kind: app.Kind, ID: app.ID, Host: app.Host}
}

// loginApplicationView reads the current name and icon of the application a
// reference names, for the pages after an upstream sign-in. A reference to an
// application since removed or disabled names nothing, so the page falls back
// to its plain heading. Naming follows the login-context endpoint: the
// display name, else the host the user returns to for an OIDC client and the
// entity ID for a SAML service provider.
func (s *Server) loginApplicationView(ctx context.Context, ref *federation.AppRef) *contract.LoginApplicationView {
	q := s.loginAppQ()
	if ref == nil || q == nil {
		return nil
	}
	var label string
	switch ref.Kind {
	case "oidc_client":
		client, err := q.GetOIDCClientAny(ctx, ref.ID)
		if err != nil || client.Disabled {
			return nil
		}
		label = client.DisplayName
		if label == "" {
			label = ref.Host
		}
	case "saml_sp":
		id, err := strconv.ParseInt(ref.ID, 10, 64)
		if err != nil {
			return nil
		}
		sp, err := q.GetSAMLSPByID(ctx, id)
		if err != nil || sp.Disabled {
			return nil
		}
		label = sp.DisplayName
		if label == "" {
			label = sp.EntityID
		}
	default:
		return nil
	}
	if label == "" {
		return nil
	}
	view := &contract.LoginApplicationView{Label: label, Host: ref.Host}
	if icon := uploadedIconOr(ctx, q, ref.Kind, ref.ID, ""); icon != "" {
		view.IconURL = &icon
	}
	return view
}
