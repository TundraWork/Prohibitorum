package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// launchpadQueries is the narrow DB surface buildLaunchpad needs. Tests stub it
// via s.launchpadOverride; production falls back to s.queries.
type launchpadQueries interface {
	ListAuthorizedOIDCClientsForAccount(ctx context.Context, accountID pgtype.Int4) ([]db.ListAuthorizedOIDCClientsForAccountRow, error)
	ListAuthorizedForwardAuthAppsForAccount(ctx context.Context, accountID pgtype.Int4) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error)
	ListAuthorizedSAMLSPsForAccount(ctx context.Context, accountID pgtype.Int4) ([]db.ListAuthorizedSAMLSPsForAccountRow, error)
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

// buildLaunchpad merges the three authorized sources into one name-sorted list.
func (s *Server) buildLaunchpad(ctx context.Context, accountID int32) ([]contract.LaunchpadApp, error) {
	q := s.getLaunchpadQueries()
	acct := pgtype.Int4{Int32: accountID, Valid: true}
	out := make([]contract.LaunchpadApp, 0, 16)

	// iconMeta returns the icon URL and the stored backdrop accent for an entity.
	// When a row exists but has no accent yet (legacy icon uploaded before this
	// feature), the accent is computed once from the stored PNG and persisted —
	// best-effort, production only (s.queries is nil under the test stub).
	iconMeta := func(kind, id string) (url *string, accent *string) {
		m, err := q.GetEntityIconMeta(ctx, db.GetEntityIconMetaParams{OwnerKind: kind, OwnerID: id})
		if err != nil {
			return nil, nil // no icon (or lookup error — best-effort)
		}
		url = entityIconURLPtr(kind, id, m.Etag)
		if m.AccentColor.Valid && m.AccentColor.String != "" {
			a := m.AccentColor.String
			return url, &a
		}
		if s.queries != nil {
			if ic, e := s.queries.GetEntityIcon(ctx, db.GetEntityIconParams{OwnerKind: kind, OwnerID: id}); e == nil {
				if hex, e2 := branding.AccentColorBytes(ic.Png); e2 == nil {
					_ = s.queries.SetEntityIconAccent(ctx, db.SetEntityIconAccentParams{
						OwnerKind: kind, OwnerID: id, AccentColor: pgtype.Text{String: hex, Valid: true},
					})
					return url, &hex
				}
			}
		}
		return url, nil
	}

	oidc, err := q.ListAuthorizedOIDCClientsForAccount(ctx, acct)
	if err != nil {
		return nil, fmt.Errorf("launchpad: list oidc: %w", err)
	}
	for _, c := range oidc {
		launch := resolveOIDCLaunchURL(c.LaunchUrl.String, c.RedirectUris)
		if launch == "" {
			continue
		}
		iconURL, accent := iconMeta("oidc_client", c.ClientID)
		out = append(out, contract.LaunchpadApp{
			Kind: "oidc", ID: c.ClientID, Name: c.DisplayName,
			IconURL: iconURL, AccentColor: accent, LaunchURL: launch,
		})
	}

	fwd, err := q.ListAuthorizedForwardAuthAppsForAccount(ctx, acct)
	if err != nil {
		return nil, fmt.Errorf("launchpad: list forward-auth: %w", err)
	}
	for _, c := range fwd {
		if !c.ForwardAuthHost.Valid || c.ForwardAuthHost.String == "" {
			continue
		}
		iconURL, accent := iconMeta("oidc_client", c.ClientID)
		out = append(out, contract.LaunchpadApp{
			Kind: "forward_auth", ID: c.ClientID, Name: c.DisplayName,
			IconURL: iconURL, AccentColor: accent, LaunchURL: "https://" + c.ForwardAuthHost.String + "/",
		})
	}

	saml, err := q.ListAuthorizedSAMLSPsForAccount(ctx, acct)
	if err != nil {
		return nil, fmt.Errorf("launchpad: list saml: %w", err)
	}
	for _, sp := range saml {
		id := strconv.FormatInt(sp.ID, 10)
		iconURL, accent := iconMeta("saml_sp", id)
		out = append(out, contract.LaunchpadApp{
			Kind: "saml", ID: id, Name: sp.DisplayName,
			IconURL:   iconURL,
			AccentColor: accent,
			LaunchURL: "/saml/sso/init?sp=" + url.QueryEscape(sp.EntityID),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
