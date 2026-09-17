package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// managerAssignmentQueries is the generated query surface used by the scoped
// application-manager endpoints. Forward-auth apps deliberately share the OIDC
// manager table because their backing application is an OIDC client.
type managerAssignmentQueries interface {
	GetOIDCClientAny(context.Context, string) (db.OidcClient, error)
	GetSAMLSPByID(context.Context, int64) (db.SamlSp, error)
	GetAccountByIDForUpdate(context.Context, int32) (db.Account, error)
	ListActiveAppManagerCandidates(context.Context, string) ([]db.ListActiveAppManagerCandidatesRow, error)
	ListOIDCClientManagers(context.Context, string) ([]db.ListOIDCClientManagersRow, error)
	ListSAMLSPManagers(context.Context, int64) ([]db.ListSAMLSPManagersRow, error)
	AssignOIDCClientManager(context.Context, db.AssignOIDCClientManagerParams) error
	AssignSAMLSPManager(context.Context, db.AssignSAMLSPManagerParams) error
	RemoveOIDCClientManager(context.Context, db.RemoveOIDCClientManagerParams) (int64, error)
	RemoveSAMLSPManager(context.Context, db.RemoveSAMLSPManagerParams) (int64, error)
}

func (s *Server) handleListAppManagerCandidatesHTTP(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	rows, err := s.managerAssignmentQ().ListActiveAppManagerCandidates(r.Context(), query)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list application manager candidates: %w", err))
		return
	}
	items := make([]contract.AccountSummaryView, 0, len(rows))
	for _, row := range rows {
		items = append(items, contract.AccountSummaryView{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName})
	}
	writeJSON(w, buildPage(items, ""))
}

// managerAssignmentTx keeps validation and an assignment insert in the same
// transaction. Its target-account lock serializes assignment against account
// demotion, which deletes that account's manager rows in its own transaction.
type managerAssignmentTx interface {
	Queries() managerAssignmentQueries
	Commit(context.Context) error
	Rollback(context.Context) error
}

type managerAssignmentTxRunner interface {
	BeginManagerAssignmentTx(context.Context) (managerAssignmentTx, error)
}

type pgManagerAssignmentTx struct {
	tx      pgx.Tx
	queries managerAssignmentQueries
}

func (tx *pgManagerAssignmentTx) Queries() managerAssignmentQueries { return tx.queries }
func (tx *pgManagerAssignmentTx) Commit(ctx context.Context) error  { return tx.tx.Commit(ctx) }
func (tx *pgManagerAssignmentTx) Rollback(ctx context.Context) error {
	return tx.tx.Rollback(ctx)
}

func (s *Server) beginManagerAssignmentTx(ctx context.Context) (managerAssignmentTx, error) {
	if s.managerAssignmentTxRunnerOverride != nil {
		return s.managerAssignmentTxRunnerOverride.BeginManagerAssignmentTx(ctx)
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgManagerAssignmentTx{tx: tx, queries: s.queries.WithTx(tx)}, nil
}

func (s *Server) managerAssignmentQ() managerAssignmentQueries {
	if s.managerAssignmentQueriesOverride != nil {
		return s.managerAssignmentQueriesOverride
	}
	return s.queries
}

type managerAssignmentBody struct {
	AccountID int32 `json:"accountId"`
}

type managerAppRef struct {
	kind         appaccess.AppKind
	oidcClientID string
	samlSPID     int64
}

func (s *Server) validateOIDCManagerApp(ctx context.Context, clientID string, kind appaccess.AppKind) error {
	if clientID == "" {
		return authn.ErrBadRequest()
	}
	client, err := s.managerAssignmentQ().GetOIDCClientAny(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return authn.ErrClientNotFound()
		}
		return fmt.Errorf("validate OIDC manager app: %w", err)
	}
	if (kind == appaccess.KindOIDC && client.ForwardAuthEnabled) ||
		(kind == appaccess.KindForwardAuth && !client.ForwardAuthEnabled) {
		return authn.ErrClientNotFound()
	}
	return nil
}

func (s *Server) validateSAMLManagerApp(ctx context.Context, idParam string) (int64, error) {
	if idParam == "" {
		return 0, authn.ErrBadRequest()
	}
	id, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return 0, authn.ErrBadRequest()
	}
	if _, err := s.managerAssignmentQ().GetSAMLSPByID(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, samlSPNotFound()
		}
		return 0, fmt.Errorf("validate SAML manager app: %w", err)
	}
	return id, nil
}

func validateManagerAssignmentTarget(ctx context.Context, q managerAssignmentQueries, accountID int32) error {
	account, err := q.GetAccountByIDForUpdate(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return authn.ErrAccountNotFound()
		}
		return fmt.Errorf("validate manager assignment target: %w", err)
	}
	if account.Role != "app_manager" || account.Disabled {
		return authn.ErrInvalidManagerRole()
	}
	return nil
}

func oidcManagerViews(rows []db.ListOIDCClientManagersRow) []contract.AppManagerView {
	views := make([]contract.AppManagerView, 0, len(rows))
	for _, row := range rows {
		views = append(views, contract.AppManagerView{
			ID:          row.AccountID,
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Disabled:    row.Disabled,
			AssignedAt:  row.CreatedAt.Time,
		})
	}
	return views
}

func samlManagerViews(rows []db.ListSAMLSPManagersRow) []contract.AppManagerView {
	views := make([]contract.AppManagerView, 0, len(rows))
	for _, row := range rows {
		views = append(views, contract.AppManagerView{
			ID:          row.AccountID,
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Disabled:    row.Disabled,
			AssignedAt:  row.CreatedAt.Time,
		})
	}
	return views
}

func (s *Server) handleListOIDCApplicationManagersHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, false)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindOIDC); err != nil {
		writeAuthErr(w, err)
		return
	}
	rows, err := s.managerAssignmentQ().ListOIDCClientManagers(r.Context(), clientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list OIDC application managers: %w", err))
		return
	}
	writeJSON(w, oidcManagerViews(rows))
}

func (s *Server) handleListForwardAuthAppManagersHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, true)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindForwardAuth); err != nil {
		writeAuthErr(w, err)
		return
	}
	rows, err := s.managerAssignmentQ().ListOIDCClientManagers(r.Context(), clientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list forward-auth application managers: %w", err))
		return
	}
	writeJSON(w, oidcManagerViews(rows))
}

func (s *Server) handleListSAMLApplicationManagersHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := s.validateSAMLManagerApp(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.authorizeApplicationManager(r.Context(), samlApplicationRef(id)); err != nil {
		writeAuthErr(w, err)
		return
	}
	rows, err := s.managerAssignmentQ().ListSAMLSPManagers(r.Context(), id)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list SAML application managers: %w", err))
		return
	}
	writeJSON(w, samlManagerViews(rows))
}

func (s *Server) assignManager(w http.ResponseWriter, r *http.Request, app managerAppRef) {
	var body managerAssignmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.AccountID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	tx, err := s.beginManagerAssignmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("assign application manager: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	q := tx.Queries()
	if err := validateManagerAssignmentTarget(r.Context(), q, body.AccountID); err != nil {
		writeAuthErr(w, err)
		return
	}

	sess := authn.SessionFromContext(r.Context())
	actorID := sess.Account.ID
	createdBy := pgtype.Int4{Int32: actorID, Valid: true}
	switch app.kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		err = q.AssignOIDCClientManager(r.Context(), db.AssignOIDCClientManagerParams{
			ClientID: app.oidcClientID, AccountID: body.AccountID, CreatedBy: createdBy,
		})
	case appaccess.KindSAML:
		err = q.AssignSAMLSPManager(r.Context(), db.AssignSAMLSPManagerParams{
			SamlSpID: app.samlSPID, AccountID: body.AccountID, CreatedBy: createdBy,
		})
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("assign application manager: %w", err))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("assign application manager: commit: %w", err))
		return
	}
	s.recordManagerAssignmentAudit(r.Context(), app, body.AccountID, audit.EventAppManagerAssigned)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeManager(w http.ResponseWriter, r *http.Request, app managerAppRef) {
	var body managerAssignmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.AccountID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	q := s.managerAssignmentQ()
	var (
		rows int64
		err  error
	)
	switch app.kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		rows, err = q.RemoveOIDCClientManager(r.Context(), db.RemoveOIDCClientManagerParams{
			ClientID: app.oidcClientID, AccountID: body.AccountID,
		})
	case appaccess.KindSAML:
		rows, err = q.RemoveSAMLSPManager(r.Context(), db.RemoveSAMLSPManagerParams{
			SamlSpID: app.samlSPID, AccountID: body.AccountID,
		})
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("remove application manager: %w", err))
		return
	}
	if rows == 0 {
		if app.kind == appaccess.KindSAML {
			writeAuthErr(w, samlSPNotFound())
		} else {
			writeAuthErr(w, authn.ErrClientNotFound())
		}
		return
	}
	s.recordManagerAssignmentAudit(r.Context(), app, body.AccountID, audit.EventAppManagerRemoved)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recordManagerAssignmentAudit(ctx context.Context, app managerAppRef, targetAccountID int32, event string) {
	var appID any
	if app.kind == appaccess.KindSAML {
		appID = app.samlSPID
	} else {
		appID = app.oidcClientID
	}
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(authn.SessionFromContext(ctx)),
		Factor:    audit.FactorAppManager,
		Event:     event,
		Detail: map[string]any{
			"target_account_id": targetAccountID,
			"app_kind":          string(app.kind),
			"app_id":            appID,
		},
	})
}

func (s *Server) handleAssignOIDCApplicationManagerHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, false)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindOIDC); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.assignManager(w, r, managerAppRef{kind: appaccess.KindOIDC, oidcClientID: clientID})
}

func (s *Server) handleAssignForwardAuthAppManagerHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, true)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindForwardAuth); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.assignManager(w, r, managerAppRef{kind: appaccess.KindForwardAuth, oidcClientID: clientID})
}

func (s *Server) handleAssignSAMLApplicationManagerHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := s.validateSAMLManagerApp(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.authorizeApplicationManager(r.Context(), samlApplicationRef(id)); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.assignManager(w, r, managerAppRef{kind: appaccess.KindSAML, samlSPID: id})
}

func (s *Server) handleRemoveOIDCApplicationManagerHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, false)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindOIDC); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.removeManager(w, r, managerAppRef{kind: appaccess.KindOIDC, oidcClientID: clientID})
}

func (s *Server) handleRemoveForwardAuthAppManagerHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if err := s.authorizeApplicationManager(r.Context(), oidcApplicationRef(clientID, true)); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.validateOIDCManagerApp(r.Context(), clientID, appaccess.KindForwardAuth); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.removeManager(w, r, managerAppRef{kind: appaccess.KindForwardAuth, oidcClientID: clientID})
}

func (s *Server) handleRemoveSAMLApplicationManagerHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := s.validateSAMLManagerApp(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.authorizeApplicationManager(r.Context(), samlApplicationRef(id)); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.removeManager(w, r, managerAppRef{kind: appaccess.KindSAML, samlSPID: id})
}
