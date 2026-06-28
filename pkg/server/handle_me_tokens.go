package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// patQueries is the narrow DB surface the PAT handlers require.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring (NewServer) leaves patQueriesOverride nil and handlers
// fall back to s.queries.
type patQueries interface {
	InsertPAT(ctx context.Context, arg db.InsertPATParams) (db.PersonalAccessToken, error)
	ListPATsByAccount(ctx context.Context, accountID int32) ([]db.PersonalAccessToken, error)
	RevokePAT(ctx context.Context, arg db.RevokePATParams) (int64, error)
}

// patView projects a db.PersonalAccessToken into the public-safe shape.
func patView(row db.PersonalAccessToken) contract.PersonalAccessTokenView {
	v := contract.PersonalAccessTokenView{
		ID:               row.ID,
		Name:             row.Name,
		TokenHint:        row.TokenHint,
		UpstreamScopes:   append([]string(nil), row.UpstreamScopes...),
		AllowedClientIDs: append([]string(nil), row.AllowedClientIds...),
		CreatedAt:        row.CreatedAt.Time,
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		v.ExpiresAt = &t
	}
	if row.LastUsedAt.Valid {
		t := row.LastUsedAt.Time
		v.LastUsedAt = &t
	}
	return v
}

// nonNilStrings returns a non-nil slice so sqlc binds a text[] NOT NULL column.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ----- GET /me/tokens -----------------------------------------------------

type listMyTokensOut struct {
	Body []contract.PersonalAccessTokenView
}

func (s *Server) patQueriesFn() patQueries {
	if s.patQueriesOverride != nil {
		return s.patQueriesOverride
	}
	return s.queries
}

func (s *Server) handleListMyTokens(ctx context.Context, _ *struct{}) (*listMyTokensOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	q := s.patQueriesFn()
	rows, err := q.ListPATsByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListMyTokens: %w", err)
	}
	out := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, row := range rows {
		out = append(out, patView(row))
	}
	return &listMyTokensOut{Body: out}, nil
}

// ----- POST /me/tokens (sudo) --------------------------------------------

type createMyTokenIn struct {
	Body struct {
		Name             string   `json:"name"`
		ExpiresInDays    *int     `json:"expiresInDays,omitempty"` // nil/0 = no expiry
		UpstreamScopes   []string `json:"upstreamScopes,omitempty"`
		AllowedClientIDs []string `json:"allowedClientIds,omitempty"`
	}
}

type createMyTokenOut struct {
	Body contract.PersonalAccessTokenCreated
}

func (s *Server) handleCreateMyToken(ctx context.Context, in *createMyTokenIn) (*createMyTokenOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" || len(name) > 128 {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	// Reject out-of-range expiry up front: a negative value would otherwise
	// fall through the >0 check below and silently mint a no-expiry (immortal)
	// token; an absurdly large value would push AddDate past any meaningful
	// timestamp. 3650 days (~10 years) is the sanity cap. nil/0 = no expiry.
	if d := in.Body.ExpiresInDays; d != nil && (*d < 0 || *d > 3650) {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	raw, hash, hint, err := pat.Generate()
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: generate: %w", err)
	}
	var expires pgtype.Timestamptz
	if in.Body.ExpiresInDays != nil && *in.Body.ExpiresInDays > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, *in.Body.ExpiresInDays), Valid: true}
	}
	q := s.patQueriesFn()
	row, err := q.InsertPAT(ctx, db.InsertPATParams{
		AccountID:        sess.Account.ID,
		Name:             name,
		TokenHash:        hash,
		TokenHint:        hint,
		UpstreamScopes:   nonNilStrings(in.Body.UpstreamScopes),
		AllowedClientIds: nonNilStrings(in.Body.AllowedClientIDs),
		ExpiresAt:        expires,
	})
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: insert: %w", err)
	}
	credRef := int64(row.ID)
	_ = s.Audit.Record(ctx, audit.Record{
		AccountID:     &sess.Account.ID,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRegister,
		CredentialRef: &credRef,
		Detail:        map[string]any{"name": name},
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event": "auth.pat_created", "account_id": sess.Account.ID, "pat_id": row.ID,
	}).Info("auth")
	return &createMyTokenOut{Body: contract.PersonalAccessTokenCreated{Token: raw, PAT: patView(row)}}, nil
}

// ----- POST /me/tokens/revoke --------------------------------------------

type revokeMyTokenIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

func (s *Server) handleRevokeMyToken(ctx context.Context, in *revokeMyTokenIn) (*emptyOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	q := s.patQueriesFn()
	n, err := q.RevokePAT(ctx, db.RevokePATParams{ID: in.Body.ID, AccountID: sess.Account.ID})
	if err != nil {
		return nil, fmt.Errorf("handleRevokeMyToken: %w", err)
	}
	if n == 0 {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}
	credRef := int64(in.Body.ID)
	_ = s.Audit.Record(ctx, audit.Record{
		AccountID:     &sess.Account.ID,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRevoke,
		CredentialRef: &credRef,
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event": "auth.pat_revoked", "account_id": sess.Account.ID, "pat_id": in.Body.ID,
	}).Info("auth")
	return &emptyOut{}, nil
}
