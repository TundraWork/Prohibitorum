package server

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

type consentMgmtQueries interface {
	ListConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListConsentsByAccountRow, error)
	DeleteConsent(ctx context.Context, arg db.DeleteConsentParams) error
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
	rows, err := q.ListConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents: %w", err)
	}
	out := make([]contract.ConsentedApp, 0, len(rows))
	for _, r := range rows {
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "oidc_client", OwnerID: r.ClientID}); e == nil {
			iconURL = entityIconURLPtr("oidc_client", r.ClientID, etag)
		}
		out = append(out, contract.ConsentedApp{
			ClientID:  r.ClientID,
			Name:      r.DisplayName,
			IconURL:   iconURL,
			Scopes:    append([]string(nil), r.GrantedScopes...),
			GrantedAt: r.UpdatedAt.Time,
		})
	}
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
	if err := s.revokeConsent(ctx, sess.Account.ID, in.Body.ClientID); err != nil {
		return nil, err
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":      "auth.consent_revoked_self",
		"account_id": sess.Account.ID,
		"client_id":  in.Body.ClientID,
	}).Info("auth")
	return &struct{}{}, nil
}

func (s *Server) revokeConsent(ctx context.Context, accountID int32, clientID string) error {
	if err := s.getConsentMgmtQueries().DeleteConsent(ctx, db.DeleteConsentParams{
		AccountID: accountID, ClientID: clientID,
	}); err != nil {
		return fmt.Errorf("revokeConsent: %w", err)
	}
	return nil
}
