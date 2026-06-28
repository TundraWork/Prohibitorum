package server

import (
	"context"
	"encoding/json"
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
	ListAuthorizedForwardAuthAppsForAccount(ctx context.Context, accountID pgtype.Int4) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error)
}

// patView projects a row, unmarshalling app_grants (jsonb) to a map.
func patView(row db.PersonalAccessToken) contract.PersonalAccessTokenView {
	grants := map[string][]string{}
	if len(row.AppGrants) > 0 {
		_ = json.Unmarshal(row.AppGrants, &grants)
	}
	v := contract.PersonalAccessTokenView{
		ID: row.ID, Name: row.Name, TokenHint: row.TokenHint,
		AllApps: row.AllApps, AppGrants: grants, CreatedAt: row.CreatedAt.Time,
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

// parseFAScopes unmarshals an app's forward_auth_scopes jsonb into the wire shape.
func parseFAScopes(raw []byte) []contract.ForwardAuthScope {
	out := []contract.ForwardAuthScope{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
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
		Name          string              `json:"name"`
		ExpiresInDays *int                `json:"expiresInDays,omitempty"`
		AllApps       bool                `json:"allApps"`
		AppGrants     map[string][]string `json:"appGrants"`
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
	if d := in.Body.ExpiresInDays; d != nil && (*d < 0 || *d > 3650) {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}

	q := s.patQueriesFn()
	grants := in.Body.AppGrants
	if grants == nil {
		grants = map[string][]string{}
	}
	if in.Body.AllApps {
		if len(grants) > 0 { // all_apps is identity-only
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
	} else {
		if len(grants) == 0 { // least-privilege: must pick ≥1 app
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
		// Build the owner's authorized app -> allowed-scope-set map.
		rows, err := q.ListAuthorizedForwardAuthAppsForAccount(ctx, pgtype.Int4{Int32: sess.Account.ID, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: authorized apps: %w", err)
		}
		vocab := map[string]map[string]bool{}
		for _, r := range rows {
			set := map[string]bool{}
			for _, sc := range parseFAScopes(r.ForwardAuthScopes) {
				set[sc.Name] = true
			}
			vocab[r.ClientID] = set
		}
		for cid, scopes := range grants {
			allowed, ok := vocab[cid]
			if !ok {
				return nil, authErrToHuma(authn.ErrBadRequest()) // not an authorized app
			}
			for _, sc := range scopes {
				if !allowed[sc] {
					return nil, authErrToHuma(authn.ErrBadRequest()) // scope not in vocabulary
				}
			}
		}
	}

	raw, hash, hint, err := pat.Generate()
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: generate: %w", err)
	}
	grantsJSON, _ := json.Marshal(grants)
	var expires pgtype.Timestamptz
	if in.Body.ExpiresInDays != nil && *in.Body.ExpiresInDays > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, *in.Body.ExpiresInDays), Valid: true}
	}
	row, err := q.InsertPAT(ctx, db.InsertPATParams{
		AccountID: sess.Account.ID, Name: name, TokenHash: hash, TokenHint: hint,
		AllApps: in.Body.AllApps, AppGrants: grantsJSON, ExpiresAt: expires,
	})
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: insert: %w", err)
	}
	credRef := int64(row.ID)
	_ = s.Audit.Record(ctx, audit.Record{
		AccountID: &sess.Account.ID, Factor: audit.FactorPAT, Event: audit.EventRegister,
		CredentialRef: &credRef, Detail: map[string]any{"name": name},
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{"event": "auth.pat_created", "account_id": sess.Account.ID, "pat_id": row.ID}).Info("auth")
	return &createMyTokenOut{Body: contract.PersonalAccessTokenCreated{Token: raw, PAT: patView(row)}}, nil
}

// ----- GET /me/forward-auth-apps -----------------------------------------

type listMyFAAppsOut struct {
	Body []contract.MyForwardAuthApp
}

func (s *Server) handleListMyForwardAuthApps(ctx context.Context, _ *struct{}) (*listMyFAAppsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.patQueriesFn().ListAuthorizedForwardAuthAppsForAccount(ctx, pgtype.Int4{Int32: sess.Account.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("handleListMyForwardAuthApps: %w", err)
	}
	out := make([]contract.MyForwardAuthApp, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.MyForwardAuthApp{
			ClientID: r.ClientID, DisplayName: r.DisplayName, Scopes: parseFAScopes(r.ForwardAuthScopes),
		})
	}
	return &listMyFAAppsOut{Body: out}, nil
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
