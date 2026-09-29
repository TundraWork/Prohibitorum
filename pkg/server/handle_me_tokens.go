package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/appaccess"
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
	InsertPATApp(ctx context.Context, arg db.InsertPATAppParams) error
	ListPATAppsByPATIDs(ctx context.Context, patIds []int32) ([]db.ListPATAppsByPATIDsRow, error)
	RevokePAT(ctx context.Context, arg db.RevokePATParams) (int64, error)
	// GetAccountByID backs the admin account-existence 404 guard on
	// GET /accounts/{id}/tokens (handle_admin_account_tokens.go), mirroring the
	// sibling GET /accounts/{id}/* handlers in handle_account.go.
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
}

// patAppLister loads the applications of selected_apps tokens.
type patAppLister interface {
	ListPATAppsByPATIDs(ctx context.Context, patIds []int32) ([]db.ListPATAppsByPATIDsRow, error)
}

// patView projects a token row and its application list (nil unless the token
// is selected_apps) into the wire view. apps is always serialized as an array.
func patView(row db.PersonalAccessToken, apps []contract.PersonalAccessTokenApp) contract.PersonalAccessTokenView {
	if apps == nil {
		apps = []contract.PersonalAccessTokenApp{}
	}
	v := contract.PersonalAccessTokenView{
		ID: row.ID, Name: row.Name, TokenHint: row.TokenHint,
		Access: row.Access, Apps: apps, CreatedAt: row.CreatedAt.Time,
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

// patViews projects rows with one query for all their application lists. The
// query orders by display name, so each token's apps keep that order.
func patViews(ctx context.Context, q patAppLister, rows []db.PersonalAccessToken) ([]contract.PersonalAccessTokenView, error) {
	ids := make([]int32, 0, len(rows))
	for _, row := range rows {
		if pat.Access(row.Access) == pat.AccessSelectedApps {
			ids = append(ids, row.ID)
		}
	}
	byPAT := map[int32][]contract.PersonalAccessTokenApp{}
	if len(ids) > 0 {
		appRows, err := q.ListPATAppsByPATIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("list token apps: %w", err)
		}
		for _, ar := range appRows {
			byPAT[ar.PatID] = append(byPAT[ar.PatID], contract.PersonalAccessTokenApp{ClientID: ar.ClientID, DisplayName: ar.DisplayName})
		}
	}
	out := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, row := range rows {
		out = append(out, patView(row, byPAT[row.ID]))
	}
	return out, nil
}

func (s *Server) listAllowedApps(ctx context.Context, accountID int32) ([]appaccess.AppSummary, error) {
	if s.appLister == nil {
		return nil, fmt.Errorf("app lister unavailable")
	}
	return s.appLister.ListAllowedApps(ctx, accountID)
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
	out, err := patViews(ctx, q, rows)
	if err != nil {
		return nil, fmt.Errorf("handleListMyTokens: %w", err)
	}
	return &listMyTokensOut{Body: out}, nil
}

// ----- POST /me/tokens (sudo) --------------------------------------------

type createMyTokenIn struct {
	Body struct {
		Name          string   `json:"name"`
		ExpiresInDays *int     `json:"expiresInDays,omitempty"`
		Access        string   `json:"access" enum:"selected_apps,all_apps,full,sudo"`
		AppClientIDs  []string `json:"appClientIds,omitempty"`
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
	access, err := pat.ParseAccess(in.Body.Access)
	if err != nil {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}

	q := s.patQueriesFn()
	clientIDs := in.Body.AppClientIDs
	if access == pat.AccessSelectedApps {
		if len(clientIDs) == 0 { // least-privilege: must pick ≥1 app
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
		// Re-evaluate the owner's allowed apps at creation time so a stale picker
		// cannot grant a now-denied application.
		apps, err := s.listAllowedApps(ctx, sess.Account.ID)
		if err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: allowed apps: %w", err)
		}
		allowed := make(map[string]bool, len(apps))
		for _, app := range apps {
			if app.Ref.Kind == appaccess.KindForwardAuth {
				allowed[app.Ref.OIDCClientID] = true
			}
		}
		seen := make(map[string]bool, len(clientIDs))
		for _, cid := range clientIDs {
			if !allowed[cid] || seen[cid] { // not an authorized app, or listed twice
				return nil, authErrToHuma(authn.ErrBadRequest())
			}
			seen[cid] = true
		}
	} else if clientIDs != nil { // an empty list still counts as present
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
	params := db.InsertPATParams{
		AccountID: sess.Account.ID, Name: name, TokenHash: hash, TokenHint: hint,
		Access: string(access), ExpiresAt: expires,
	}

	// The token row and its application list commit together. Without a pool
	// (unit-test seam) the injected queries are used directly.
	var row db.PersonalAccessToken
	var tx pgx.Tx
	if s.dbPool != nil && s.patQueriesOverride == nil {
		tx, err = s.dbPool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: begin: %w", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		q = s.queries.WithTx(tx)
	}
	row, err = q.InsertPAT(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: insert: %w", err)
	}
	for _, cid := range clientIDs {
		if err := q.InsertPATApp(ctx, db.InsertPATAppParams{PatID: row.ID, ClientID: cid}); err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: insert app: %w", err)
		}
	}
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: commit: %w", err)
		}
	}
	// q may be bound to the finished transaction; read back through the pool.
	views, err := patViews(ctx, s.patQueriesFn(), []db.PersonalAccessToken{row})
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: %w", err)
	}
	credRef := int64(row.ID)
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: &sess.Account.ID, Factor: audit.FactorPAT, Event: audit.EventRegister,
		CredentialRef: &credRef, Detail: map[string]any{"name": name, "access": string(access)},
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{"event": "auth.pat_created", "account_id": sess.Account.ID, "pat_id": row.ID}).Info("auth")
	return &createMyTokenOut{Body: contract.PersonalAccessTokenCreated{Token: raw, PAT: views[0]}}, nil
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
	apps, err := s.listAllowedApps(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListMyForwardAuthApps: %w", err)
	}
	out := make([]contract.MyForwardAuthApp, 0, len(apps))
	for _, app := range apps {
		if app.Ref.Kind != appaccess.KindForwardAuth {
			continue
		}
		out = append(out, contract.MyForwardAuthApp{
			ClientID: app.Ref.OIDCClientID, DisplayName: app.DisplayName,
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
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
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
