package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"net/http"
	"sort"
	"strconv"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
)

// appPolicyQueries is the intentionally narrow generated-query surface shared
// by Task 3's evaluator and the app-bound management handlers. It keeps the
// focused HTTP tests database-free without widening db.Querier again.
type appPolicyQueries interface {
	GetEntityIconEtag(context.Context, db.GetEntityIconEtagParams) (string, error)
	GetOIDCClient(context.Context, string) (db.OidcClient, error)
	GetOIDCClientAny(context.Context, string) (db.OidcClient, error)
	GetOIDCClientAnyForUpdate(context.Context, string) (db.OidcClient, error)
	GetSAMLSPByID(context.Context, int64) (db.SamlSp, error)
	GetSAMLSPByIDForUpdate(context.Context, int64) (db.SamlSp, error)
	GetAccountAccessFacts(context.Context, int32) (db.GetAccountAccessFactsRow, error)
	ListManualDecisionsForOIDCApp(context.Context, db.ListManualDecisionsForOIDCAppParams) ([]db.GroupManualDecision, error)
	ListManualDecisionsForSAMLApp(context.Context, db.ListManualDecisionsForSAMLAppParams) ([]db.GroupManualDecision, error)
	GetOIDCAppGroup(context.Context, db.GetOIDCAppGroupParams) (db.UserGroup, error)
	GetSAMLAppGroup(context.Context, db.GetSAMLAppGroupParams) (db.UserGroup, error)
	ListOIDCAppGroups(context.Context, string) ([]db.UserGroup, error)
	ListSAMLAppGroups(context.Context, int64) ([]db.UserGroup, error)
	ListOIDCAppRuleGroups(context.Context, string) ([]db.UserGroup, error)
	ListSAMLAppRuleGroups(context.Context, int64) ([]db.UserGroup, error)
	ListKnownUpstreamIDPSlugs(context.Context) ([]string, error)
	IsOIDCClientManager(context.Context, db.IsOIDCClientManagerParams) (bool, error)
	IsSAMLSPManager(context.Context, db.IsSAMLSPManagerParams) (bool, error)
	ListOIDCAccessCandidates(context.Context) ([]db.ListOIDCAccessCandidatesRow, error)
	ListForwardAuthAccessCandidates(context.Context) ([]db.ListForwardAuthAccessCandidatesRow, error)
	ListSAMLAccessCandidates(context.Context) ([]db.ListSAMLAccessCandidatesRow, error)
	ListOIDCManagementCandidates(context.Context) ([]db.ListOIDCManagementCandidatesRow, error)
	ListForwardAuthManagementCandidates(context.Context) ([]db.ListForwardAuthManagementCandidatesRow, error)
	ListSAMLManagementCandidates(context.Context) ([]db.ListSAMLManagementCandidatesRow, error)
	ListActiveAccountAccessFactsPage(context.Context, db.ListActiveAccountAccessFactsPageParams) ([]db.ListActiveAccountAccessFactsPageRow, error)
	ListActiveAccountAccessFacts(context.Context) ([]db.ListActiveAccountAccessFactsRow, error)
	ListKnownUpstreamIDPDescriptors(context.Context) ([]db.ListKnownUpstreamIDPDescriptorsRow, error)
	CreateGlobalGroup(context.Context, db.CreateGlobalGroupParams) (db.UserGroup, error)
	GetGlobalGroup(context.Context, int32) (db.UserGroup, error)
	ListGlobalGroups(context.Context) ([]db.UserGroup, error)
	ListGlobalManualDecisionsForAccount(context.Context, int32) ([]db.GroupManualDecision, error)
	UpdateGlobalGroup(context.Context, db.UpdateGlobalGroupParams) (db.UserGroup, error)
	DeleteGlobalGroup(context.Context, int32) (int64, error)
	ReplaceOIDCAppGroups(context.Context, db.ReplaceOIDCAppGroupsParams) ([]db.UserGroup, error)
	ReplaceSAMLAppGroups(context.Context, db.ReplaceSAMLAppGroupsParams) ([]db.UserGroup, error)
	CreateOIDCAppGroup(context.Context, db.CreateOIDCAppGroupParams) (db.UserGroup, error)
	CreateSAMLAppGroup(context.Context, db.CreateSAMLAppGroupParams) (db.UserGroup, error)
	UpdateAppGroup(context.Context, db.UpdateAppGroupParams) (db.UserGroup, error)
	DeleteOIDCAppGroup(context.Context, db.DeleteOIDCAppGroupParams) (int64, error)
	DeleteSAMLAppGroup(context.Context, db.DeleteSAMLAppGroupParams) (int64, error)
	ListManualDecisionsPage(context.Context, db.ListManualDecisionsPageParams) ([]db.ListManualDecisionsPageRow, error)
	UpsertManualDecision(context.Context, db.UpsertManualDecisionParams) (db.GroupManualDecision, error)
	ClearManualDecision(context.Context, db.ClearManualDecisionParams) (int64, error)
	SetOIDCClientAccessRestricted(context.Context, db.SetOIDCClientAccessRestrictedParams) (db.OidcClient, error)
	SetSAMLSPAccessRestricted(context.Context, db.SetSAMLSPAccessRestrictedParams) (db.SamlSp, error)
}

// appPolicyService is the Task 3 evaluator surface used by this HTTP layer.
type appPolicyService interface {
	AuthorizeManager(context.Context, int32, string, appaccess.AppRef) error
	ListAccountGroups(context.Context, int32) ([]db.UserGroup, error)
	PreviewGroup(context.Context, appaccess.AppRef, int32, db.ListActiveAccountAccessFactsPageParams) ([]appaccess.GroupPreview, error)
	PreviewRule(context.Context, appaccess.AppRef, appaccess.Rule) ([]appaccess.GroupPreview, error)
	ExplainGroup(context.Context, appaccess.AppRef, int32, int32) (appaccess.Explanation, error)
}

type appPolicyTx interface {
	Queries() appPolicyQueries
	Commit(context.Context) error
	Rollback(context.Context) error
}

type appPolicyTxRunner interface {
	BeginAppPolicyTx(context.Context) (appPolicyTx, error)
}

type pgAppPolicyTx struct {
	tx      pgx.Tx
	queries appPolicyQueries
}

func (tx *pgAppPolicyTx) Queries() appPolicyQueries          { return tx.queries }
func (tx *pgAppPolicyTx) Commit(ctx context.Context) error   { return tx.tx.Commit(ctx) }
func (tx *pgAppPolicyTx) Rollback(ctx context.Context) error { return tx.tx.Rollback(ctx) }

func (s *Server) beginAppPolicyTx(ctx context.Context) (appPolicyTx, error) {
	if s.appPolicyTxRunnerOverride != nil {
		return s.appPolicyTxRunnerOverride.BeginAppPolicyTx(ctx)
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgAppPolicyTx{tx: tx, queries: s.queries.WithTx(tx)}, nil
}

func (s *Server) appPolicyQ() appPolicyQueries {
	if s.appPolicyQueriesOverride != nil {
		return s.appPolicyQueriesOverride
	}
	return s.queries
}

func (s *Server) appPolicyEvaluator() appPolicyService {
	if s.appPolicyService != nil {
		return s.appPolicyService
	}
	return appaccess.NewService(s.appPolicyQ())
}

// authorizeApplicationManager applies object-level authorization for existing
// application management endpoints. Admins are accepted by the shared evaluator;
// other accounts must hold the exact kind/id assignment.
func (s *Server) authorizeApplicationManager(ctx context.Context, ref appaccess.AppRef) error {
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Account == nil {
		return authn.ErrNoSession()
	}
	return s.appPolicyEvaluator().AuthorizeManager(ctx, sess.Account.ID, sess.Account.Role, ref)
}

func oidcApplicationRef(clientID string, forwardAuth bool) appaccess.AppRef {
	kind := appaccess.KindOIDC
	if forwardAuth {
		kind = appaccess.KindForwardAuth
	}
	return appaccess.AppRef{Kind: kind, OIDCClientID: clientID}
}

func samlApplicationRef(id int64) appaccess.AppRef {
	return appaccess.AppRef{Kind: appaccess.KindSAML, SAMLSPID: id}
}

// registerManagedApplicationRoutes is kept separate from registerOperations so
// focused route tests exercise the exact production registration table.
func (s *Server) registerManagedApplicationRoutes(router chiRouter) {
	const base = "/api/prohibitorum/managed-applications"
	req := contract.AuthRequirement{Kind: contract.AuthSession}

	registerOpHTTP(router, http.MethodGet, base, req, s.handleListManagedApplicationsHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/access", req, s.handleManagedApplicationAccessWorkspaceHTTP)
	s.registerAdminBodyOpHTTP(router, http.MethodPost, base+"/{kind}/{appId}/rule-preview", req, s.handlePreviewManagedRuleHTTP)
	s.registerAdminBodyOpHTTP(router, http.MethodPost, base+"/{kind}/{appId}/access/set-restricted", req, s.handleSetManagedApplicationRestrictedHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/groups", req, s.handleListManagedApplicationGroupsHTTP)
	s.registerAdminBodyOpHTTP(router, http.MethodPut, base+"/{kind}/{appId}/groups", req, s.handleReplaceManagedApplicationGroupsHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/groups/{groupId}", req, s.handleGetManagedApplicationGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/groups/{groupId}/preview", req, s.handlePreviewManagedGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/groups/{groupId}/explain/{accountId}", req, s.handleExplainManagedGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{kind}/{appId}/accounts", req, s.handleListManagedApplicationAccountsHTTP)
}

type replaceManagedApplicationGroupsBody struct {
	GroupIDs []int32 `json:"groupIds"`
}

func (s *Server) handleReplaceManagedApplicationGroupsHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var body replaceManagedApplicationGroupsBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	seen := make(map[int32]struct{}, len(body.GroupIDs))
	for _, id := range body.GroupIDs {
		if id <= 0 {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		if _, duplicate := seen[id]; duplicate {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		seen[id] = struct{}{}
	}
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Account == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	tx, err := s.beginAppPolicyTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("replace managed application groups: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := tx.Queries()
	if err := lockApplicationForGroupReplacement(r.Context(), q, app.ref); err != nil {
		writeAuthErr(w, err)
		return
	}
	evaluator := appaccess.NewService(q)
	if err := evaluator.AuthorizeManager(r.Context(), sess.Account.ID, sess.Account.Role, app.ref); err != nil {
		if errors.Is(err, appaccess.ErrAppNotFound) {
			writeAuthErr(w, authn.ErrClientNotFound())
		} else {
			writeAuthErr(w, fmt.Errorf("reauthorize managed application: %w", err))
		}
		return
	}
	if err := evaluator.ValidateGroupReplacement(r.Context(), sess.Account.ID, sess.Account.Role, app.ref, body.GroupIDs); err != nil {
		switch {
		case errors.Is(err, appaccess.ErrGroupNotFound):
			writeAuthErr(w, authn.ErrGroupNotFound())
		case errors.Is(err, appaccess.ErrGroupOutOfScope):
			writeAuthErr(w, authn.ErrPermissionDenied())
		default:
			writeAuthErr(w, fmt.Errorf("validate managed application groups: %w", err))
		}
		return
	}

	var groups []db.UserGroup
	switch app.ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		groups, err = q.ReplaceOIDCAppGroups(r.Context(), db.ReplaceOIDCAppGroupsParams{
			OidcClientID: app.ref.OIDCClientID, GroupIds: body.GroupIDs,
		})
	case appaccess.KindSAML:
		groups, err = q.ReplaceSAMLAppGroups(r.Context(), db.ReplaceSAMLAppGroupsParams{
			SamlSpID: app.ref.SAMLSPID, GroupIds: body.GroupIDs,
		})
	default:
		err = pgx.ErrNoRows
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeAuthErr(w, authn.ErrGroupNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("replace managed application groups: %w", err))
		return
	}
	providers := make(map[string]struct{})
	for _, group := range groups {
		if group.Kind != "rule" {
			continue
		}
		providerSlugs, err := q.ListKnownUpstreamIDPSlugs(r.Context())
		if err != nil {
			writeAuthErr(w, fmt.Errorf("list known providers for managed application groups: %w", err))
			return
		}
		providers = providerSet(providerSlugs)
		break
	}
	views, err := s.appGroupViewsWithProviders(r.Context(), groups, providers)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("replace managed application groups: commit: %w", err))
		return
	}
	s.recordAppPolicy(r.Context(), app.ref, audit.EventUpdate, map[string]any{"group_ids": body.GroupIDs})
	writeJSON(w, views)
}

func lockApplicationForGroupReplacement(ctx context.Context, q appPolicyQueries, ref appaccess.AppRef) error {
	switch ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		client, err := q.GetOIDCClientAnyForUpdate(ctx, ref.OIDCClientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return authn.ErrClientNotFound()
		}
		if err != nil {
			return fmt.Errorf("lock managed OIDC application: %w", err)
		}
		if (ref.Kind == appaccess.KindOIDC && client.ForwardAuthEnabled) || (ref.Kind == appaccess.KindForwardAuth && !client.ForwardAuthEnabled) {
			return authn.ErrClientNotFound()
		}
		return nil
	case appaccess.KindSAML:
		_, err := q.GetSAMLSPByIDForUpdate(ctx, ref.SAMLSPID)
		if errors.Is(err, pgx.ErrNoRows) {
			return authn.ErrClientNotFound()
		}
		if err != nil {
			return fmt.Errorf("lock managed SAML application: %w", err)
		}
		return nil
	default:
		return authn.ErrClientNotFound()
	}
}

type managedApplication struct {
	ref     appaccess.AppRef
	summary contract.AppSummaryView
}

func managedAppRefFromRequest(r *http.Request) (appaccess.AppRef, error) {
	kind := appaccess.AppKind(chi.URLParam(r, "kind"))
	appID := chi.URLParam(r, "appId")
	switch kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		if appID == "" {
			return appaccess.AppRef{}, appaccess.ErrAppNotFound
		}
		return appaccess.AppRef{Kind: kind, OIDCClientID: appID}, nil
	case appaccess.KindSAML:
		id, err := strconv.ParseInt(appID, 10, 64)
		if err != nil || id <= 0 {
			return appaccess.AppRef{}, appaccess.ErrAppNotFound
		}
		return appaccess.AppRef{Kind: kind, SAMLSPID: id}, nil
	default:
		return appaccess.AppRef{}, appaccess.ErrAppNotFound
	}
}

// managedApplicationFromRequest performs authorization before any handler-level
// application or group lookup. appaccess.AuthorizeManager deliberately returns
// the same sentinel for invalid refs and absent assignments; preserve that
// non-enumerating boundary as client_not_found.
func (s *Server) managedApplicationFromRequest(r *http.Request) (managedApplication, error) {
	ref, err := managedAppRefFromRequest(r)
	if err != nil {
		return managedApplication{}, authn.ErrClientNotFound()
	}
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Account == nil {
		return managedApplication{}, authn.ErrNoSession()
	}
	if err := s.appPolicyEvaluator().AuthorizeManager(r.Context(), sess.Account.ID, sess.Account.Role, ref); err != nil {
		if errors.Is(err, appaccess.ErrAppNotFound) {
			return managedApplication{}, authn.ErrClientNotFound()
		}
		return managedApplication{}, fmt.Errorf("authorize managed application: %w", err)
	}
	summary, err := s.loadManagedApplication(r.Context(), ref)
	if err != nil {
		return managedApplication{}, err
	}
	iconKind := "oidc_client"
	if ref.Kind == appaccess.KindSAML {
		iconKind = "saml_sp"
	}
	etag, err := s.appPolicyQ().GetEntityIconEtag(r.Context(), db.GetEntityIconEtagParams{OwnerKind: iconKind, OwnerID: summary.AppID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return managedApplication{}, fmt.Errorf("load managed application icon: %w", err)
	}
	summary.IconURL = entityIconURLPtr(iconKind, summary.AppID, etag)
	return managedApplication{ref: ref, summary: summary}, nil
}

func (s *Server) loadManagedApplication(ctx context.Context, ref appaccess.AppRef) (contract.AppSummaryView, error) {
	switch ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		client, err := s.appPolicyQ().GetOIDCClientAny(ctx, ref.OIDCClientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return contract.AppSummaryView{}, authn.ErrClientNotFound()
		}
		if err != nil {
			return contract.AppSummaryView{}, fmt.Errorf("load managed OIDC application: %w", err)
		}
		if (ref.Kind == appaccess.KindOIDC && client.ForwardAuthEnabled) || (ref.Kind == appaccess.KindForwardAuth && !client.ForwardAuthEnabled) {
			return contract.AppSummaryView{}, authn.ErrClientNotFound()
		}
		return managedOIDCSummary(ref.Kind, client)
	case appaccess.KindSAML:
		sp, err := s.appPolicyQ().GetSAMLSPByID(ctx, ref.SAMLSPID)
		if errors.Is(err, pgx.ErrNoRows) {
			return contract.AppSummaryView{}, authn.ErrClientNotFound()
		}
		if err != nil {
			return contract.AppSummaryView{}, fmt.Errorf("load managed SAML application: %w", err)
		}
		return contract.AppSummaryView{
			Kind: string(ref.Kind), AppID: strconv.FormatInt(sp.ID, 10), DisplayName: sp.DisplayName,
			EntityID: sp.EntityID, AccessRestricted: sp.AccessRestricted,
		}, nil
	default:
		return contract.AppSummaryView{}, authn.ErrClientNotFound()
	}
}

func managedOIDCSummary(kind appaccess.AppKind, client db.OidcClient) (contract.AppSummaryView, error) {
	view := contract.AppSummaryView{
		Kind: string(kind), AppID: client.ClientID, DisplayName: client.DisplayName,
		AccessRestricted: client.AccessRestricted,
	}
	if kind == appaccess.KindForwardAuth {
		if client.ForwardAuthHost.Valid {
			view.ForwardAuthHost = client.ForwardAuthHost.String
		}
		if len(client.ForwardAuthScopes) > 0 {
			if err := json.Unmarshal(client.ForwardAuthScopes, &view.ForwardAuthScopes); err != nil {
				return contract.AppSummaryView{}, fmt.Errorf("decode forward-auth scopes: %w", err)
			}
		}
		return view, nil
	}
	if client.LaunchUrl.Valid {
		view.LaunchURL = client.LaunchUrl.String
	}
	view.RedirectURIs = append([]string(nil), client.RedirectUris...)
	return view, nil
}

func (s *Server) handleListManagedApplicationsHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Account == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	apps, err := s.listManagedApplications(r.Context(), sess.Account.ID, sess.Account.Role)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, apps)
}

func (s *Server) listManagedApplications(ctx context.Context, accountID int32, role string) ([]contract.AppSummaryView, error) {
	q := s.appPolicyQ()
	oidc, err := q.ListOIDCManagementCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed OIDC applications: %w", err)
	}
	forward, err := q.ListForwardAuthManagementCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed forward-auth applications: %w", err)
	}
	saml, err := q.ListSAMLManagementCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed SAML applications: %w", err)
	}

	apps := make([]contract.AppSummaryView, 0, len(oidc)+len(forward)+len(saml))
	appendIfAuthorized := func(ref appaccess.AppRef, summary contract.AppSummaryView) error {
		if role != "admin" {
			err := s.appPolicyEvaluator().AuthorizeManager(ctx, accountID, role, ref)
			if errors.Is(err, appaccess.ErrAppNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("authorize managed application list item: %w", err)
			}
		}
		apps = append(apps, summary)
		return nil
	}
	for _, row := range oidc {
		ref := appaccess.AppRef{Kind: appaccess.KindOIDC, OIDCClientID: row.ClientID}
		summary := contract.AppSummaryView{Kind: string(ref.Kind), AppID: row.ClientID, DisplayName: row.DisplayName, AccessRestricted: row.AccessRestricted, RedirectURIs: append([]string(nil), row.RedirectUris...)}
		if row.LaunchUrl.Valid {
			summary.LaunchURL = row.LaunchUrl.String
		}
		if err := appendIfAuthorized(ref, summary); err != nil {
			return nil, err
		}
	}
	for _, row := range forward {
		ref := appaccess.AppRef{Kind: appaccess.KindForwardAuth, OIDCClientID: row.ClientID}
		summary := contract.AppSummaryView{Kind: string(ref.Kind), AppID: row.ClientID, DisplayName: row.DisplayName, AccessRestricted: row.AccessRestricted}
		if row.ForwardAuthHost.Valid {
			summary.ForwardAuthHost = row.ForwardAuthHost.String
		}
		if len(row.ForwardAuthScopes) > 0 {
			if err := json.Unmarshal(row.ForwardAuthScopes, &summary.ForwardAuthScopes); err != nil {
				return nil, fmt.Errorf("decode forward-auth scopes for %q: %w", row.ClientID, err)
			}
		}
		if err := appendIfAuthorized(ref, summary); err != nil {
			return nil, err
		}
	}
	for _, row := range saml {
		ref := appaccess.AppRef{Kind: appaccess.KindSAML, SAMLSPID: row.ID}
		summary := contract.AppSummaryView{Kind: string(ref.Kind), AppID: strconv.FormatInt(row.ID, 10), DisplayName: row.DisplayName, EntityID: row.EntityID, AccessRestricted: row.AccessRestricted}
		if err := appendIfAuthorized(ref, summary); err != nil {
			return nil, err
		}
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].DisplayName == apps[j].DisplayName {
			if apps[i].Kind == apps[j].Kind {
				return apps[i].AppID < apps[j].AppID
			}
			return apps[i].Kind < apps[j].Kind
		}
		return apps[i].DisplayName < apps[j].DisplayName
	})
	return apps, nil
}

func (s *Server) handleManagedApplicationAccessWorkspaceHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groups, err := s.listBoundAppGroups(r.Context(), app.ref)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	providerSlugs, err := s.knownProviderSlugs(r.Context())
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	views, err := s.appGroupViewsWithProviders(r.Context(), groups, providerSet(providerSlugs))
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	descriptors, err := s.appPolicyQ().ListKnownUpstreamIDPDescriptors(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list known upstream provider descriptors: %w", err))
		return
	}
	providers := make([]contract.ProviderDescriptorView, len(descriptors))
	for i, descriptor := range descriptors {
		providers[i] = contract.ProviderDescriptorView{Slug: descriptor.Slug, DisplayName: descriptor.DisplayName}
	}
	workspace := contract.AppAccessWorkspace{App: app.summary, AccessRestricted: app.summary.AccessRestricted, Providers: providers, Groups: views}
	writeJSON(w, workspace)
}

type previewManagedRuleBody struct {
	Version   int             `json:"version"`
	Condition json.RawMessage `json:"condition"`
	Cursor    string          `json:"cursor"`
	Limit     int             `json:"limit"`
}

func decodePreviewManagedRuleBody(r *http.Request, dst *previewManagedRuleBody) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return authn.ErrBadRequest()
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return authn.ErrBadRequest()
	}
	return nil
}

func (s *Server) handlePreviewManagedRuleHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var body previewManagedRuleBody
	if err := decodePreviewManagedRuleBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	rawRule, err := json.Marshal(map[string]any{"version": body.Version, "condition": body.Condition})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("encode rule preview request: %w", err))
		return
	}
	canonicalRule, err := s.canonicalRule(r.Context(), rawRule)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var rule appaccess.Rule
	if err := json.Unmarshal(canonicalRule, &rule); err != nil {
		writeAuthErr(w, fmt.Errorf("decode canonical rule preview: %w", err))
		return
	}
	limit := pagination.Limit(body.Limit)
	const collection = "managed_rule_preview"
	const sortID = "username"
	digest := sha256.Sum256(canonicalRule)
	filters := managedAppCursorFilters(app.ref)
	filters["ruleHash"] = hex.EncodeToString(digest[:])
	payload, err := s.decodeCursor(body.Cursor, collection, sortID, filters)
	if err != nil {
		writeCursorInvalidErr(w, err)
		return
	}
	previews, err := s.appPolicyEvaluator().PreviewRule(r.Context(), app.ref, rule)
	if err != nil {
		writeAuthErr(w, appPolicyReadErr(err))
		return
	}
	matchedCount := 0
	for _, preview := range previews {
		if preview.Matched {
			matchedCount++
		}
	}
	start := 0
	if body.Cursor != "" {
		afterUsername, afterAccountID := decodeASCTextIntKey(payload.Keys)
		for start < len(previews) {
			account := previews[start].Account
			if account.Username > afterUsername || (account.Username == afterUsername && account.ID > afterAccountID) {
				break
			}
			start++
		}
	}
	end := start + limit
	if end > len(previews) {
		end = len(previews)
	}
	items := make([]contract.GroupPreviewView, 0, end-start)
	for _, preview := range previews[start:end] {
		items = append(items, contract.GroupPreviewView{
			Account: contract.AccountSummaryView{ID: preview.Account.ID, Username: preview.Account.Username, DisplayName: preview.Account.DisplayName},
			Matched: preview.Matched,
		})
	}
	next := ""
	if end < len(previews) && len(items) > 0 {
		last := items[len(items)-1].Account
		next = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.Username, last.ID))
	}
	writeJSON(w, contract.RulePreviewPageView{Items: items, MatchedCount: matchedCount, NextCursor: next})
}

func (s *Server) handleListManagedApplicationGroupsHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groups, err := s.listBoundAppGroups(r.Context(), app.ref)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	views, err := s.appGroupViews(r.Context(), groups)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, views)
}

func (s *Server) handleListManagedApplicationAccountsHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	limit, err := appPolicyLimit(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	const collection = "managed_application_accounts"
	const sortID = "username"
	filters := managedAppCursorFilters(app.ref)
	payload, err := s.decodeCursor(r.URL.Query().Get("cursor"), collection, sortID, filters)
	if err != nil {
		writeCursorInvalidErr(w, err)
		return
	}
	afterUsername, afterAccountID := decodeASCTextIntKey(payload.Keys)
	rows, err := s.appPolicyQ().ListActiveAccountAccessFactsPage(r.Context(), db.ListActiveAccountAccessFactsPageParams{
		AfterUsername: pgText(afterUsername), AfterAccountID: pgInt4(afterAccountID), RowLimit: int32(limit + 1),
	})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list managed application accounts: %w", err))
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]contract.AccountSummaryView, 0, len(rows))
	for _, row := range rows {
		items = append(items, accountSummaryFromPage(row))
	}
	next := ""
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.Username, last.ID))
	}
	writeJSON(w, buildPage(items, next))
}

func appPolicyLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return pagination.Limit(0), nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, authn.ErrBadRequest()
	}
	return pagination.Limit(limit), nil
}

func managedAppCursorFilters(ref appaccess.AppRef) map[string]string {
	filters := map[string]string{"appKind": string(ref.Kind)}
	if ref.Kind == appaccess.KindSAML {
		filters["appId"] = strconv.FormatInt(ref.SAMLSPID, 10)
	} else {
		filters["appId"] = ref.OIDCClientID
	}
	return filters
}

func accountSummaryFromFacts(row db.GetAccountAccessFactsRow) contract.AccountSummaryView {
	return contract.AccountSummaryView{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}
}

func accountSummaryFromPage(row db.ListActiveAccountAccessFactsPageRow) contract.AccountSummaryView {
	return contract.AccountSummaryView{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}
}

func pgText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func pgInt4(value int32) pgtype.Int4 {
	if value == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: value, Valid: true}
}
