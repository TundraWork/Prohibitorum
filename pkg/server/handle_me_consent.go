package server

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

type consentMgmtQueries interface {
	ListConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListConsentsByAccountRow, error)
	DeleteConsent(ctx context.Context, arg db.DeleteConsentParams) error
	ListSAMLConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListSAMLConsentsByAccountRow, error)
	DeleteSAMLConsent(ctx context.Context, arg db.DeleteSAMLConsentParams) error
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

func (s *Server) getConsentMgmtQueries() consentMgmtQueries {
	if s.consentMgmtOverride != nil {
		return s.consentMgmtOverride
	}
	return s.queries
}

type consentListOut struct {
	Body []contract.ConsentedApp
}

func (s *Server) handleListMyConsent(ctx context.Context, _ *struct{}) (*consentListOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	out, err := s.listConsents(ctx, sess.Account.ID)
	if err != nil {
		return nil, err
	}
	return &consentListOut{Body: out}, nil
}

func (s *Server) listConsents(ctx context.Context, accountID int32) ([]contract.ConsentedApp, error) {
	q := s.getConsentMgmtQueries()

	oidc, err := q.ListConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents oidc: %w", err)
	}
	out := make([]contract.ConsentedApp, 0, len(oidc))
	for _, r := range oidc {
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "oidc_client", OwnerID: r.ClientID}); e == nil {
			iconURL = entityIconURLPtr("oidc_client", r.ClientID, etag)
		}
		out = append(out, contract.ConsentedApp{
			Kind:      "oidc",
			ClientID:  r.ClientID,
			Name:      r.DisplayName,
			IconURL:   iconURL,
			Scopes:    append([]string(nil), r.GrantedScopes...),
			GrantedAt: r.UpdatedAt.Time,
		})
	}

	saml, err := q.ListSAMLConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents saml: %w", err)
	}
	for _, r := range saml {
		id := strconv.FormatInt(r.SpID, 10)
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "saml_sp", OwnerID: id}); e == nil {
			iconURL = entityIconURLPtr("saml_sp", id, etag)
		}
		out = append(out, contract.ConsentedApp{
			Kind:      "saml",
			ClientID:  id,
			Name:      r.DisplayName,
			IconURL:   iconURL,
			// Empty (non-nil) so the JSON is "scopes":[] not "scopes":null — SAML
			// acks carry no scopes, but a null would be a footgun for consumers.
			Scopes:    []string{},
			GrantedAt: r.UpdatedAt.Time,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type revokeConsentIn struct {
	Body contract.RevokeConsentInput
}

func (s *Server) handleRevokeMyConsent(ctx context.Context, in *revokeConsentIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if in.Body.ClientID == "" {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	if err := s.revokeConsent(ctx, sess.Account.ID, in.Body.Kind, in.Body.ClientID); err != nil {
		return nil, err
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":      "auth.consent_revoked_self",
		"account_id": sess.Account.ID,
		"client_id":  in.Body.ClientID,
		"kind":       in.Body.Kind,
	}).Info("auth")
	return &struct{}{}, nil
}

func (s *Server) revokeConsent(ctx context.Context, accountID int32, kind, id string) error {
	q := s.getConsentMgmtQueries()
	if kind == "saml" {
		spID, perr := strconv.ParseInt(id, 10, 64)
		if perr != nil {
			return fmt.Errorf("revokeConsent: bad saml id %q: %w", id, perr)
		}
		if err := q.DeleteSAMLConsent(ctx, db.DeleteSAMLConsentParams{AccountID: accountID, SpID: spID}); err != nil {
			return fmt.Errorf("revokeConsent saml: %w", err)
		}
		return nil
	}
	if err := q.DeleteConsent(ctx, db.DeleteConsentParams{AccountID: accountID, ClientID: id}); err != nil {
		return fmt.Errorf("revokeConsent oidc: %w", err)
	}
	return nil
}
