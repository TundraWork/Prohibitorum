package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// launchpadQueries is the narrow DB surface used only for best-effort icon
// metadata. Application selection comes from the live app-policy service.
type launchpadQueries interface {
	GetEntityIconMeta(ctx context.Context, arg db.GetEntityIconMetaParams) (db.GetEntityIconMetaRow, error)
}

func (s *Server) getLaunchpadQueries() launchpadQueries {
	if s.launchpadOverride != nil {
		return s.launchpadOverride
	}
	return s.queries
}

type myAppsOut struct {
	Body []contract.LaunchpadApp
}

func (s *Server) handleListMyApps(ctx context.Context, _ *struct{}) (*myAppsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	apps, err := s.buildLaunchpad(ctx, sess.Account.ID)
	if err != nil {
		return nil, err
	}
	return &myAppsOut{Body: apps}, nil
}

// buildLaunchpad maps the live allowed-app projection into the existing
// launchpad contract and sorts it by display name.
func (s *Server) buildLaunchpad(ctx context.Context, accountID int32) ([]contract.LaunchpadApp, error) {
	if s.appLister == nil {
		return nil, fmt.Errorf("launchpad: app lister unavailable")
	}
	apps, err := s.appLister.ListAllowedApps(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("launchpad: list allowed apps: %w", err)
	}
	q := s.getLaunchpadQueries()
	out := make([]contract.LaunchpadApp, 0, len(apps))

	// iconMeta returns the icon URL and the stored backdrop accent for an entity.
	// When a row exists but has no accent yet (legacy icon uploaded before this
	// feature), the accent is computed once from the stored PNG and persisted —
	// best-effort, production only.
	iconMeta := func(kind, id string) (url *string, accent *string) {
		m, err := q.GetEntityIconMeta(ctx, db.GetEntityIconMetaParams{OwnerKind: kind, OwnerID: id})
		if err != nil {
			return nil, nil
		}
		url = entityIconURLPtr(kind, id, m.Etag)
		if m.AccentColor.Valid && m.AccentColor.String != "" {
			a := m.AccentColor.String
			return url, &a
		}
		if s.queries != nil {
			if ic, iconErr := s.queries.GetEntityIcon(ctx, db.GetEntityIconParams{OwnerKind: kind, OwnerID: id}); iconErr == nil {
				if hex, accentErr := branding.AccentColorBytes(ic.Png); accentErr == nil {
					_ = s.queries.SetEntityIconAccent(ctx, db.SetEntityIconAccentParams{
						OwnerKind: kind, OwnerID: id, AccentColor: pgtype.Text{String: hex, Valid: true},
					})
					return url, &hex
				}
			}
		}
		return url, nil
	}

	for _, app := range apps {
		var item contract.LaunchpadApp
		switch app.Ref.Kind {
		case appaccess.KindOIDC:
			launch := resolveOIDCLaunchURL(app.LaunchURL, app.RedirectURIs)
			if launch == "" {
				continue
			}
			iconURL, accent := iconMeta("oidc_client", app.Ref.OIDCClientID)
			item = contract.LaunchpadApp{
				Kind: "oidc", ID: app.Ref.OIDCClientID, Name: app.DisplayName,
				IconURL: iconURL, AccentColor: accent, LaunchURL: launch,
			}
		case appaccess.KindForwardAuth:
			if app.ForwardAuthHost == "" {
				continue
			}
			iconURL, accent := iconMeta("oidc_client", app.Ref.OIDCClientID)
			item = contract.LaunchpadApp{
				Kind: "forward_auth", ID: app.Ref.OIDCClientID, Name: app.DisplayName,
				IconURL: iconURL, AccentColor: accent, LaunchURL: "https://" + app.ForwardAuthHost + "/",
			}
		case appaccess.KindSAML:
			id := strconv.FormatInt(app.Ref.SAMLSPID, 10)
			iconURL, accent := iconMeta("saml_sp", id)
			item = contract.LaunchpadApp{
				Kind: "saml", ID: id, Name: app.DisplayName,
				IconURL:     iconURL,
				AccentColor: accent,
				LaunchURL:   "/saml/sso/init?sp=" + url.QueryEscape(app.EntityID),
			}
		default:
			return nil, fmt.Errorf("launchpad: unsupported app kind %q", app.Ref.Kind)
		}
		out = append(out, item)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
