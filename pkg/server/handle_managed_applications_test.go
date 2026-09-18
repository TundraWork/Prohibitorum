package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

type policyAuditCapture struct {
	records []audit.Record
}

func (*policyTestQueries) GetEntityIconEtag(context.Context, db.GetEntityIconEtagParams) (string, error) {
	return "", pgx.ErrNoRows
}

func (c *policyAuditCapture) Record(_ context.Context, record audit.Record) error {
	c.records = append(c.records, record)
	return nil
}

// policyTestQueries is a stateful fake over the exact Task 1 query surface
// consumed by the Task 3 evaluator and Task 5 HTTP handlers. It deliberately
// models application bindings so tests cannot accidentally permit a global
// group operation.
type policyTestQueries struct {
	oidc                map[string]db.OidcClient
	saml                map[int64]db.SamlSp
	oidcManagers        map[string]map[int32]bool
	samlManagers        map[int64]map[int32]bool
	groups              map[int32]db.UserGroup
	oidcGroups          map[string]map[int32]bool
	samlGroups          map[int64]map[int32]bool
	decisions           map[int32]map[int32]db.GroupManualDecision
	accounts            map[int32]db.GetAccountAccessFactsRow
	providers           []string
	providerDescriptors []db.ListKnownUpstreamIDPDescriptorsRow
	nextGroupID         int32

	appLookupCalls   int
	groupLookupCalls int
	mutationCalls    int
	activeFactsCalls int
}

func newPolicyTestQueries() *policyTestQueries {
	return &policyTestQueries{
		oidc:         make(map[string]db.OidcClient),
		saml:         make(map[int64]db.SamlSp),
		oidcManagers: make(map[string]map[int32]bool),
		samlManagers: make(map[int64]map[int32]bool),
		groups:       make(map[int32]db.UserGroup),
		oidcGroups:   make(map[string]map[int32]bool),
		samlGroups:   make(map[int64]map[int32]bool),
		decisions:    make(map[int32]map[int32]db.GroupManualDecision),
		accounts:     make(map[int32]db.GetAccountAccessFactsRow),
		providers:    []string{"github"},
		nextGroupID:  10,
	}
}

func (q *policyTestQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	q.appLookupCalls++
	client, ok := q.oidc[clientID]
	if !ok || client.Disabled {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return client, nil
}

func (q *policyTestQueries) GetOIDCClientAny(_ context.Context, clientID string) (db.OidcClient, error) {
	q.appLookupCalls++
	client, ok := q.oidc[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return client, nil
}

func (q *policyTestQueries) GetSAMLSPByID(_ context.Context, id int64) (db.SamlSp, error) {
	q.appLookupCalls++
	sp, ok := q.saml[id]
	if !ok {
		return db.SamlSp{}, pgx.ErrNoRows
	}
	return sp, nil
}

func (q *policyTestQueries) GetAccountAccessFacts(_ context.Context, id int32) (db.GetAccountAccessFactsRow, error) {
	account, ok := q.accounts[id]
	if !ok {
		return db.GetAccountAccessFactsRow{}, pgx.ErrNoRows
	}
	return account, nil
}

func (q *policyTestQueries) ListManualDecisionsForOIDCApp(_ context.Context, arg db.ListManualDecisionsForOIDCAppParams) ([]db.GroupManualDecision, error) {
	var out []db.GroupManualDecision
	for groupID, group := range q.groups {
		if group.Kind != "manual" || !q.oidcGroups[arg.OidcClientID][groupID] {
			continue
		}
		if decision, ok := q.decisions[groupID][arg.AccountID]; ok {
			out = append(out, decision)
		}
	}
	return out, nil
}

func (q *policyTestQueries) ListManualDecisionsForSAMLApp(_ context.Context, arg db.ListManualDecisionsForSAMLAppParams) ([]db.GroupManualDecision, error) {
	var out []db.GroupManualDecision
	for groupID, group := range q.groups {
		if group.Kind != "manual" || !q.samlGroups[arg.SamlSpID][groupID] {
			continue
		}
		if decision, ok := q.decisions[groupID][arg.AccountID]; ok {
			out = append(out, decision)
		}
	}
	return out, nil
}

func (q *policyTestQueries) GetOIDCAppGroup(_ context.Context, arg db.GetOIDCAppGroupParams) (db.UserGroup, error) {
	q.groupLookupCalls++
	group, ok := q.groups[arg.GroupID]
	if !ok || !q.oidcGroups[arg.OidcClientID][arg.GroupID] {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	return group, nil
}

func (q *policyTestQueries) GetSAMLAppGroup(_ context.Context, arg db.GetSAMLAppGroupParams) (db.UserGroup, error) {
	q.groupLookupCalls++
	group, ok := q.groups[arg.GroupID]
	if !ok || !q.samlGroups[arg.SamlSpID][arg.GroupID] {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	return group, nil
}

func (q *policyTestQueries) ListOIDCAppRuleGroups(_ context.Context, clientID string) ([]db.UserGroup, error) {
	return q.listGroups(func(group db.UserGroup) bool {
		return group.Kind == "rule" && q.oidcGroups[clientID][group.ID]
	}), nil
}

func (q *policyTestQueries) ListSAMLAppRuleGroups(_ context.Context, spID int64) ([]db.UserGroup, error) {
	return q.listGroups(func(group db.UserGroup) bool {
		return group.Kind == "rule" && q.samlGroups[spID][group.ID]
	}), nil
}

func (q *policyTestQueries) ListKnownUpstreamIDPSlugs(context.Context) ([]string, error) {
	return append([]string(nil), q.providers...), nil
}

func (q *policyTestQueries) ListKnownUpstreamIDPDescriptors(context.Context) ([]db.ListKnownUpstreamIDPDescriptorsRow, error) {
	if q.providerDescriptors != nil {
		return append([]db.ListKnownUpstreamIDPDescriptorsRow(nil), q.providerDescriptors...), nil
	}
	descriptors := make([]db.ListKnownUpstreamIDPDescriptorsRow, len(q.providers))
	for i, slug := range q.providers {
		descriptors[i] = db.ListKnownUpstreamIDPDescriptorsRow{Slug: slug, DisplayName: slug}
	}
	return descriptors, nil
}

func (q *policyTestQueries) IsOIDCClientManager(_ context.Context, arg db.IsOIDCClientManagerParams) (bool, error) {
	return q.oidcManagers[arg.ClientID][arg.AccountID], nil
}

func (q *policyTestQueries) IsSAMLSPManager(_ context.Context, arg db.IsSAMLSPManagerParams) (bool, error) {
	return q.samlManagers[arg.SamlSpID][arg.AccountID], nil
}

func (q *policyTestQueries) ListOIDCAccessCandidates(context.Context) ([]db.ListOIDCAccessCandidatesRow, error) {
	rows := make([]db.ListOIDCAccessCandidatesRow, 0, len(q.oidc))
	for _, client := range q.oidc {
		if client.Disabled || client.ForwardAuthEnabled {
			continue
		}
		rows = append(rows, db.ListOIDCAccessCandidatesRow{
			ClientID: client.ClientID, DisplayName: client.DisplayName, LaunchUrl: client.LaunchUrl,
			RedirectUris: client.RedirectUris, AccessRestricted: client.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ClientID < rows[j].ClientID })
	return rows, nil
}

func (q *policyTestQueries) ListForwardAuthAccessCandidates(context.Context) ([]db.ListForwardAuthAccessCandidatesRow, error) {
	rows := make([]db.ListForwardAuthAccessCandidatesRow, 0, len(q.oidc))
	for _, client := range q.oidc {
		if client.Disabled || !client.ForwardAuthEnabled {
			continue
		}
		rows = append(rows, db.ListForwardAuthAccessCandidatesRow{
			ClientID: client.ClientID, DisplayName: client.DisplayName, ForwardAuthHost: client.ForwardAuthHost,
			ForwardAuthScopes: client.ForwardAuthScopes, AccessRestricted: client.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ClientID < rows[j].ClientID })
	return rows, nil
}

func (q *policyTestQueries) ListSAMLAccessCandidates(context.Context) ([]db.ListSAMLAccessCandidatesRow, error) {
	rows := make([]db.ListSAMLAccessCandidatesRow, 0, len(q.saml))
	for _, sp := range q.saml {
		if sp.Disabled || !sp.AllowIdpInitiated {
			continue
		}
		rows = append(rows, db.ListSAMLAccessCandidatesRow{
			ID: sp.ID, EntityID: sp.EntityID, DisplayName: sp.DisplayName, AccessRestricted: sp.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

func (q *policyTestQueries) ListOIDCManagementCandidates(context.Context) ([]db.ListOIDCManagementCandidatesRow, error) {
	rows := make([]db.ListOIDCManagementCandidatesRow, 0, len(q.oidc))
	for _, client := range q.oidc {
		if client.ForwardAuthEnabled {
			continue
		}
		rows = append(rows, db.ListOIDCManagementCandidatesRow{
			ClientID: client.ClientID, DisplayName: client.DisplayName, LaunchUrl: client.LaunchUrl,
			RedirectUris: client.RedirectUris, AccessRestricted: client.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ClientID < rows[j].ClientID })
	return rows, nil
}

func (q *policyTestQueries) ListForwardAuthManagementCandidates(context.Context) ([]db.ListForwardAuthManagementCandidatesRow, error) {
	rows := make([]db.ListForwardAuthManagementCandidatesRow, 0, len(q.oidc))
	for _, client := range q.oidc {
		if !client.ForwardAuthEnabled {
			continue
		}
		rows = append(rows, db.ListForwardAuthManagementCandidatesRow{
			ClientID: client.ClientID, DisplayName: client.DisplayName, ForwardAuthHost: client.ForwardAuthHost,
			ForwardAuthScopes: client.ForwardAuthScopes, AccessRestricted: client.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ClientID < rows[j].ClientID })
	return rows, nil
}

func (q *policyTestQueries) ListSAMLManagementCandidates(context.Context) ([]db.ListSAMLManagementCandidatesRow, error) {
	rows := make([]db.ListSAMLManagementCandidatesRow, 0, len(q.saml))
	for _, sp := range q.saml {
		rows = append(rows, db.ListSAMLManagementCandidatesRow{
			ID: sp.ID, EntityID: sp.EntityID, DisplayName: sp.DisplayName, AccessRestricted: sp.AccessRestricted,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

func (q *policyTestQueries) ListActiveAccountAccessFactsPage(_ context.Context, arg db.ListActiveAccountAccessFactsPageParams) ([]db.ListActiveAccountAccessFactsPageRow, error) {
	rows := make([]db.ListActiveAccountAccessFactsPageRow, 0, len(q.accounts))
	for _, account := range q.accounts {
		if account.Disabled || (arg.AfterUsername.Valid && (account.Username < arg.AfterUsername.String || (account.Username == arg.AfterUsername.String && account.ID <= arg.AfterAccountID.Int32))) {
			continue
		}
		rows = append(rows, db.ListActiveAccountAccessFactsPageRow(account))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Username == rows[j].Username {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Username < rows[j].Username
	})
	if int(arg.RowLimit) < len(rows) {
		rows = rows[:arg.RowLimit]
	}
	return rows, nil
}

func (q *policyTestQueries) ListActiveAccountAccessFacts(context.Context) ([]db.ListActiveAccountAccessFactsRow, error) {
	q.activeFactsCalls++
	rows := make([]db.ListActiveAccountAccessFactsRow, 0, len(q.accounts))
	for _, account := range q.accounts {
		if account.Disabled {
			continue
		}
		rows = append(rows, db.ListActiveAccountAccessFactsRow(account))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Username == rows[j].Username {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Username < rows[j].Username
	})
	return rows, nil
}

func (q *policyTestQueries) listGroups(include func(db.UserGroup) bool) []db.UserGroup {
	groups := make([]db.UserGroup, 0, len(q.groups))
	for _, group := range q.groups {
		if include(group) {
			groups = append(groups, group)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].DisplayName == groups[j].DisplayName {
			return groups[i].ID < groups[j].ID
		}
		return groups[i].DisplayName < groups[j].DisplayName
	})
	return groups
}

func (q *policyTestQueries) ListOIDCAppGroups(_ context.Context, clientID string) ([]db.UserGroup, error) {
	return q.listGroups(func(group db.UserGroup) bool {
		return q.oidcGroups[clientID][group.ID]
	}), nil
}

func (q *policyTestQueries) ListSAMLAppGroups(_ context.Context, spID int64) ([]db.UserGroup, error) {
	return q.listGroups(func(group db.UserGroup) bool {
		return q.samlGroups[spID][group.ID]
	}), nil
}

func (q *policyTestQueries) ListGlobalGroups(context.Context) ([]db.UserGroup, error) {
	return q.listGroups(func(db.UserGroup) bool { return true }), nil
}

func (q *policyTestQueries) GetGlobalGroup(_ context.Context, id int32) (db.UserGroup, error) {
	group, ok := q.groups[id]
	if !ok {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	return group, nil
}

func (q *policyTestQueries) CreateGlobalGroup(_ context.Context, arg db.CreateGlobalGroupParams) (db.UserGroup, error) {
	group := db.UserGroup{ID: q.nextGroupID, Kind: arg.Kind, Slug: arg.Slug, DisplayName: arg.DisplayName, Description: arg.Description, ExposedToDownstream: arg.ExposedToDownstream, Rule: arg.Rule}
	q.nextGroupID++
	q.groups[group.ID] = group
	return group, nil
}

func (q *policyTestQueries) UpdateGlobalGroup(_ context.Context, arg db.UpdateGlobalGroupParams) (db.UserGroup, error) {
	group, ok := q.groups[arg.GroupID]
	if !ok {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	group.Slug, group.DisplayName, group.Description = arg.Slug, arg.DisplayName, arg.Description
	group.ExposedToDownstream, group.Rule = arg.ExposedToDownstream, arg.Rule
	q.groups[group.ID] = group
	return group, nil
}

func (q *policyTestQueries) DeleteGlobalGroup(_ context.Context, id int32) (int64, error) {
	if _, ok := q.groups[id]; !ok {
		return 0, nil
	}
	for _, links := range q.oidcGroups {
		if links[id] {
			return 0, &pgconn.PgError{Code: "23001"}
		}
	}
	for _, links := range q.samlGroups {
		if links[id] {
			return 0, &pgconn.PgError{Code: "23001"}
		}
	}
	delete(q.groups, id)
	delete(q.decisions, id)
	return 1, nil
}

func (q *policyTestQueries) ReplaceOIDCAppGroups(_ context.Context, arg db.ReplaceOIDCAppGroupsParams) ([]db.UserGroup, error) {
	links := make(map[int32]bool, len(arg.GroupIds))
	for _, id := range arg.GroupIds {
		if _, ok := q.groups[id]; !ok {
			return nil, &pgconn.PgError{Code: "23503"}
		}
		links[id] = true
	}
	q.oidcGroups[arg.OidcClientID] = links
	return q.ListOIDCAppGroups(context.Background(), arg.OidcClientID)
}

func (q *policyTestQueries) ReplaceSAMLAppGroups(_ context.Context, arg db.ReplaceSAMLAppGroupsParams) ([]db.UserGroup, error) {
	links := make(map[int32]bool, len(arg.GroupIds))
	for _, id := range arg.GroupIds {
		if _, ok := q.groups[id]; !ok {
			return nil, &pgconn.PgError{Code: "23503"}
		}
		links[id] = true
	}
	q.samlGroups[arg.SamlSpID] = links
	return q.ListSAMLAppGroups(context.Background(), arg.SamlSpID)
}

func (q *policyTestQueries) CreateOIDCAppGroup(_ context.Context, arg db.CreateOIDCAppGroupParams) (db.UserGroup, error) {
	q.mutationCalls++
	for _, group := range q.groups {
		if !q.oidcGroups[arg.OidcClientID][group.ID] {
			continue
		}
		if group.Kind == "manual" && arg.Kind == "manual" {
			return db.UserGroup{}, &pgconn.PgError{Code: "23505", ConstraintName: "user_group_oidc_manual_uq"}
		}
		if group.Slug == arg.Slug {
			return db.UserGroup{}, &pgconn.PgError{Code: "23505", ConstraintName: "user_group_oidc_slug_uq"}
		}
	}
	group := db.UserGroup{ID: q.nextGroupID, Kind: arg.Kind, Slug: arg.Slug, DisplayName: arg.DisplayName, Description: arg.Description, ExposedToDownstream: arg.ExposedToDownstream, Rule: arg.Rule}
	q.nextGroupID++
	q.groups[group.ID] = group
	if q.oidcGroups[arg.OidcClientID] == nil {
		q.oidcGroups[arg.OidcClientID] = make(map[int32]bool)
	}
	q.oidcGroups[arg.OidcClientID][group.ID] = true
	return group, nil
}

func (q *policyTestQueries) CreateSAMLAppGroup(_ context.Context, arg db.CreateSAMLAppGroupParams) (db.UserGroup, error) {
	q.mutationCalls++
	for _, group := range q.groups {
		if !q.samlGroups[arg.SamlSpID][group.ID] {
			continue
		}
		if group.Kind == "manual" && arg.Kind == "manual" {
			return db.UserGroup{}, &pgconn.PgError{Code: "23505", ConstraintName: "user_group_saml_manual_uq"}
		}
		if group.Slug == arg.Slug {
			return db.UserGroup{}, &pgconn.PgError{Code: "23505", ConstraintName: "user_group_saml_slug_uq"}
		}
	}
	group := db.UserGroup{ID: q.nextGroupID, Kind: arg.Kind, Slug: arg.Slug, DisplayName: arg.DisplayName, Description: arg.Description, ExposedToDownstream: arg.ExposedToDownstream, Rule: arg.Rule}
	q.nextGroupID++
	q.groups[group.ID] = group
	if q.samlGroups[arg.SamlSpID] == nil {
		q.samlGroups[arg.SamlSpID] = make(map[int32]bool)
	}
	q.samlGroups[arg.SamlSpID][group.ID] = true
	return group, nil
}

func (q *policyTestQueries) UpdateAppGroup(_ context.Context, arg db.UpdateAppGroupParams) (db.UserGroup, error) {
	q.mutationCalls++
	group, ok := q.groups[arg.GroupID]
	if !ok || (arg.OidcClientID.Valid && !q.oidcGroups[arg.OidcClientID.String][arg.GroupID]) || (arg.SamlSpID.Valid && !q.samlGroups[arg.SamlSpID.Int64][arg.GroupID]) {
		return db.UserGroup{}, pgx.ErrNoRows
	}
	group.Slug = arg.Slug
	group.DisplayName = arg.DisplayName
	group.Description = arg.Description
	group.ExposedToDownstream = arg.ExposedToDownstream
	group.Rule = arg.Rule
	q.groups[group.ID] = group
	return group, nil
}

func (q *policyTestQueries) DeleteOIDCAppGroup(_ context.Context, arg db.DeleteOIDCAppGroupParams) (int64, error) {
	q.mutationCalls++
	_, ok := q.groups[arg.GroupID]
	if !ok || !q.oidcGroups[arg.OidcClientID][arg.GroupID] {
		return 0, nil
	}
	delete(q.oidcGroups[arg.OidcClientID], arg.GroupID)
	return 1, nil
}

func (q *policyTestQueries) DeleteSAMLAppGroup(_ context.Context, arg db.DeleteSAMLAppGroupParams) (int64, error) {
	q.mutationCalls++
	_, ok := q.groups[arg.GroupID]
	if !ok || !q.samlGroups[arg.SamlSpID][arg.GroupID] {
		return 0, nil
	}
	delete(q.samlGroups[arg.SamlSpID], arg.GroupID)
	return 1, nil
}

func (q *policyTestQueries) ListManualDecisionsPage(_ context.Context, arg db.ListManualDecisionsPageParams) ([]db.ListManualDecisionsPageRow, error) {
	rows := make([]db.ListManualDecisionsPageRow, 0, len(q.decisions[arg.GroupID]))
	for accountID, decision := range q.decisions[arg.GroupID] {
		account, ok := q.accounts[accountID]
		if !ok || (arg.AfterUsername.Valid && (account.Username < arg.AfterUsername.String || (account.Username == arg.AfterUsername.String && account.ID <= arg.AfterAccountID.Int32))) {
			continue
		}
		rows = append(rows, db.ListManualDecisionsPageRow{GroupID: arg.GroupID, AccountID: accountID, Effect: decision.Effect, UpdatedAt: decision.UpdatedAt, Username: account.Username, DisplayName: account.DisplayName, Disabled: account.Disabled})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Username == rows[j].Username {
			return rows[i].AccountID < rows[j].AccountID
		}
		return rows[i].Username < rows[j].Username
	})
	if int(arg.RowLimit) < len(rows) {
		rows = rows[:arg.RowLimit]
	}
	return rows, nil
}

func (q *policyTestQueries) UpsertManualDecision(_ context.Context, arg db.UpsertManualDecisionParams) (db.GroupManualDecision, error) {
	q.mutationCalls++
	if q.decisions[arg.GroupID] == nil {
		q.decisions[arg.GroupID] = make(map[int32]db.GroupManualDecision)
	}
	decision := db.GroupManualDecision{GroupID: arg.GroupID, GroupKind: "manual", AccountID: arg.AccountID, Effect: arg.Effect, UpdatedAt: pgtype.Timestamptz{Time: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC), Valid: true}, CreatedBy: arg.CreatedBy}
	q.decisions[arg.GroupID][arg.AccountID] = decision
	return decision, nil
}

func (q *policyTestQueries) ClearManualDecision(_ context.Context, arg db.ClearManualDecisionParams) (int64, error) {
	q.mutationCalls++
	if _, ok := q.decisions[arg.GroupID][arg.AccountID]; !ok {
		return 0, nil
	}
	delete(q.decisions[arg.GroupID], arg.AccountID)
	return 1, nil
}

func (q *policyTestQueries) SetOIDCClientAccessRestricted(_ context.Context, arg db.SetOIDCClientAccessRestrictedParams) (db.OidcClient, error) {
	q.mutationCalls++
	client, ok := q.oidc[arg.ClientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	client.AccessRestricted = arg.AccessRestricted
	q.oidc[arg.ClientID] = client
	return client, nil
}

func (q *policyTestQueries) SetSAMLSPAccessRestricted(_ context.Context, arg db.SetSAMLSPAccessRestrictedParams) (db.SamlSp, error) {
	q.mutationCalls++
	sp, ok := q.saml[arg.SamlSpID]
	if !ok {
		return db.SamlSp{}, pgx.ErrNoRows
	}
	sp.AccessRestricted = arg.AccessRestricted
	q.saml[arg.SamlSpID] = sp
	return sp, nil
}

func newPolicyTestServer() (*Server, *policyTestQueries, *policyAuditCapture) {
	queries := newPolicyTestQueries()
	auditCapture := &policyAuditCapture{}
	router := chi.NewRouter()
	s := &Server{
		router:                   router,
		appPolicyQueriesOverride: queries,
		appPolicyService:         appaccess.NewService(queries),
		cursorCodec:              testCodec(),
		Audit:                    auditCapture,
	}
	s.registerManagedApplicationRoutes(router)
	s.registerGlobalGroupRoutes(router)
	seedPolicyFixtures(queries)
	return s, queries, auditCapture
}

func seedPolicyFixtures(q *policyTestQueries) {
	q.oidc["wiki"] = db.OidcClient{ClientID: "wiki", DisplayName: "Wiki", LaunchUrl: pgtype.Text{String: "https://wiki.example", Valid: true}, RedirectUris: []string{"https://wiki.example/callback"}}
	q.oidc["forward"] = db.OidcClient{ClientID: "forward", DisplayName: "Forward", ForwardAuthEnabled: true, ForwardAuthHost: pgtype.Text{String: "app.example", Valid: true}, ForwardAuthScopes: []byte(`[{"name":"user","description":"User"}]`)}
	q.oidc["other"] = db.OidcClient{ClientID: "other", DisplayName: "Other"}
	q.oidc["disabled-oidc"] = db.OidcClient{ClientID: "disabled-oidc", DisplayName: "Disabled OIDC", Disabled: true}
	q.oidc["disabled-forward"] = db.OidcClient{ClientID: "disabled-forward", DisplayName: "Disabled Forward", Disabled: true, ForwardAuthEnabled: true}
	q.saml[7] = db.SamlSp{ID: 7, EntityID: "urn:test:saml", DisplayName: "SAML", AllowIdpInitiated: true}
	q.saml[8] = db.SamlSp{ID: 8, EntityID: "urn:test:saml:non-idp", DisplayName: "Non-IdP SAML", AllowIdpInitiated: false}
	q.oidcManagers["wiki"] = map[int32]bool{7: true}
	q.oidcManagers["forward"] = map[int32]bool{7: true}
	q.samlManagers[7] = map[int32]bool{7: true}
	q.oidcManagers["disabled-oidc"] = map[int32]bool{7: true}
	q.oidcManagers["disabled-forward"] = map[int32]bool{7: true}
	q.samlManagers[8] = map[int32]bool{7: true}
	q.accounts[42] = db.GetAccountAccessFactsRow{ID: 42, Username: "alice", DisplayName: "Alice", HasPasskey: true, ConfirmedProviderSlugs: []string{"github"}}
	q.accounts[43] = db.GetAccountAccessFactsRow{ID: 43, Username: "bob", DisplayName: "Bob", HasFederation: true, ConfirmedProviderSlugs: []string{"github"}}
	q.groups[1] = db.UserGroup{ID: 1, Kind: "manual", Slug: "exceptions", DisplayName: "Exceptions"}
	q.groups[2] = db.UserGroup{ID: 2, Kind: "rule", Slug: "passkeys", DisplayName: "Passkeys", Rule: []byte(`{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`)}
	q.groups[3] = db.UserGroup{ID: 3, Kind: "manual", Slug: "other-manual", DisplayName: "Other Manual"}
	q.oidcGroups["wiki"] = map[int32]bool{1: true, 2: true}
	q.oidcGroups["other"] = map[int32]bool{3: true}
	q.nextGroupID = 10
}

func managedAppSession(id int32, role string, disabled bool) *authn.Session {
	return &authn.Session{Account: &db.Account{ID: id, Role: role, Disabled: disabled}, Token: "session"}
}

func managedRequest(t *testing.T, s *Server, method, path, body string, session *authn.Session) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, reqWithSession(method, path, body, "", session))
	return recorder
}

func managedURL(kind, appID, suffix string) string {
	return "/api/prohibitorum/managed-applications/" + kind + "/" + appID + suffix
}

func assertManagedAPIError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, status, recorder.Body.String())
	}
	var public weberr.PublicError
	if err := json.Unmarshal(recorder.Body.Bytes(), &public); err != nil {
		t.Fatalf("decode error envelope: %v; body: %s", err, recorder.Body.String())
	}
	if public.Code != code {
		t.Fatalf("error code = %q, want %q", public.Code, code)
	}
}

func TestManagedAppUnassignedAndMissingAreIndistinguishable(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	for _, path := range []string{
		managedURL("oidc", "other", "/access"),
		managedURL("oidc", "missing", "/access"),
	} {
		t.Run(path, func(t *testing.T) {
			rr := managedRequest(t, s, http.MethodGet, path, "", managedAppSession(7, "user", false))
			assertManagedAPIError(t, rr, http.StatusNotFound, "client_not_found")
		})
	}
}

func TestManagedApplicationRoutesEnforceScopeBeforeNestedLookup(t *testing.T) {
	routes := []struct {
		name   string
		method string
		suffix string
		body   string
	}{
		{"workspace", http.MethodGet, "/access", ""},
		{"restrict", http.MethodPost, "/access/set-restricted", `{"restricted":true}`},
		{"groups", http.MethodGet, "/groups", ""},
		{"replace", http.MethodPut, "/groups", `{"groupIds":[1,2]}`},
		{"group", http.MethodGet, "/groups/1", ""},
		{"preview", http.MethodGet, "/groups/2/preview", ""},
		{"explain", http.MethodGet, "/groups/2/explain/42", ""},
		{"accounts", http.MethodGet, "/accounts", ""},
		{"rule-preview", http.MethodPost, "/rule-preview", `{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`},
	}

	for _, route := range routes {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Run("assigned-user", func(t *testing.T) {
				s, _, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("oidc", "wiki", route.suffix), route.body, managedAppSession(7, "user", false))
				if rr.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
				}
			})
			t.Run("disabled-manager", func(t *testing.T) {
				s, queries, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("oidc", "wiki", route.suffix), route.body, managedAppSession(7, "user", true))
				assertManagedAPIError(t, rr, http.StatusForbidden, "account_disabled")
				if queries.appLookupCalls != 0 || queries.groupLookupCalls != 0 || queries.mutationCalls != 0 {
					t.Fatalf("disabled manager touched policy data: app=%d group=%d mutation=%d", queries.appLookupCalls, queries.groupLookupCalls, queries.mutationCalls)
				}
			})
			t.Run("unassigned", func(t *testing.T) {
				s, queries, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("oidc", "other", route.suffix), route.body, managedAppSession(7, "user", false))
				assertManagedAPIError(t, rr, http.StatusNotFound, "client_not_found")
				if queries.groupLookupCalls != 0 || queries.mutationCalls != 0 {
					t.Fatalf("unassigned manager touched nested policy data: group=%d mutation=%d", queries.groupLookupCalls, queries.mutationCalls)
				}
			})
			t.Run("wrong-kind", func(t *testing.T) {
				s, queries, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("forward_auth", "wiki", route.suffix), route.body, managedAppSession(7, "user", false))
				assertManagedAPIError(t, rr, http.StatusNotFound, "client_not_found")
				if queries.groupLookupCalls != 0 || queries.mutationCalls != 0 {
					t.Fatalf("wrong-kind manager touched nested policy data: group=%d mutation=%d", queries.groupLookupCalls, queries.mutationCalls)
				}
			})
			t.Run("missing", func(t *testing.T) {
				s, queries, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("oidc", "missing", route.suffix), route.body, managedAppSession(7, "user", false))
				assertManagedAPIError(t, rr, http.StatusNotFound, "client_not_found")
				if queries.groupLookupCalls != 0 || queries.mutationCalls != 0 {
					t.Fatalf("missing app touched nested policy data: group=%d mutation=%d", queries.groupLookupCalls, queries.mutationCalls)
				}
			})
		})
	}
}

func TestManagedApplicationRoutesAllowAssignedManagerAndAdmin(t *testing.T) {
	routes := []struct {
		name   string
		method string
		suffix string
		body   string
		status int
	}{
		{"workspace", http.MethodGet, "/access", "", http.StatusOK},
		{"restrict", http.MethodPost, "/access/set-restricted", `{"restricted":true}`, http.StatusOK},
		{"groups", http.MethodGet, "/groups", "", http.StatusOK},
		{"replace", http.MethodPut, "/groups", `{"groupIds":[1,2]}`, http.StatusOK},
		{"group", http.MethodGet, "/groups/1", "", http.StatusOK},
		{"preview", http.MethodGet, "/groups/2/preview", "", http.StatusOK},
		{"explain", http.MethodGet, "/groups/2/explain/42", "", http.StatusOK},
		{"accounts", http.MethodGet, "/accounts", "", http.StatusOK},
		{"rule-preview", http.MethodPost, "/rule-preview", `{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`, http.StatusOK},
	}
	for _, route := range routes {
		route := route
		for _, actor := range []struct {
			name    string
			session *authn.Session
		}{
			{"assigned-user", managedAppSession(7, "user", false)},
			{"global-admin", managedAppSession(99, "admin", false)},
		} {
			actor := actor
			t.Run(route.name+"/"+actor.name, func(t *testing.T) {
				s, _, _ := newPolicyTestServer()
				rr := managedRequest(t, s, route.method, managedURL("oidc", "wiki", route.suffix), route.body, actor.session)
				if rr.Code != route.status {
					t.Fatalf("status = %d, want %d; body: %s", rr.Code, route.status, rr.Body.String())
				}
			})
		}
	}
}

func TestListManagedApplicationsIncludesAssignedNonLaunchableApps(t *testing.T) {
	tests := []struct {
		name    string
		session *authn.Session
		want    map[string]bool
	}{
		{
			name:    "assigned-user",
			session: managedAppSession(7, "user", false),
			want: map[string]bool{
				"oidc/wiki":                     true,
				"forward_auth/forward":          true,
				"oidc/disabled-oidc":            true,
				"forward_auth/disabled-forward": true,
				"saml/7":                        true,
				"saml/8":                        true,
			},
		},
		{name: "unassigned-user", session: managedAppSession(8, "user", false), want: map[string]bool{}},
		{
			name:    "global-admin",
			session: managedAppSession(99, "admin", false),
			want: map[string]bool{
				"oidc/wiki":                     true,
				"forward_auth/forward":          true,
				"oidc/other":                    true,
				"oidc/disabled-oidc":            true,
				"forward_auth/disabled-forward": true,
				"saml/7":                        true,
				"saml/8":                        true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := newPolicyTestServer()
			rr := managedRequest(t, s, http.MethodGet, "/api/prohibitorum/managed-applications", "", test.session)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
			}
			var apps []contract.AppSummaryView
			if err := json.Unmarshal(rr.Body.Bytes(), &apps); err != nil {
				t.Fatalf("decode managed applications: %v; body: %s", err, rr.Body.String())
			}
			got := make(map[string]bool, len(apps))
			for _, app := range apps {
				got[app.Kind+"/"+app.AppID] = true
			}
			if len(got) != len(test.want) {
				t.Fatalf("managed applications = %#v, want %#v", got, test.want)
			}
			for app := range test.want {
				if !got[app] {
					t.Fatalf("managed applications = %#v, missing %q", got, app)
				}
			}
		})
	}

	s, _, _ := newPolicyTestServer()
	assertManagedAPIError(t, managedRequest(t, s, http.MethodGet, "/api/prohibitorum/managed-applications", "", managedAppSession(7, "user", true)), http.StatusForbidden, "account_disabled")
}

func TestManagedApplicationAccountsProjectPageRows(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/accounts?limit=1"), "", managedAppSession(7, "user", false))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var page contract.Page[contract.AccountSummaryView]
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode account page: %v; body: %s", err, rr.Body.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("account items = %#v, want one", page.Items)
	}
	if got := page.Items[0]; got.ID != 42 || got.Username != "alice" || got.DisplayName != "Alice" {
		t.Fatalf("account item = %#v, want safe alice summary", got)
	}
	if page.NextCursor == "" {
		t.Fatal("account page with a second row must provide a next cursor")
	}
}

func TestManagedApplicationWorkspaceProjectsProviderDescriptors(t *testing.T) {
	s, queries, _ := newPolicyTestServer()
	queries.providers = []string{"disabled-idp", "github"}
	queries.providerDescriptors = []db.ListKnownUpstreamIDPDescriptorsRow{
		{Slug: "disabled-idp", DisplayName: "Disabled Invite-only Provider"},
		{Slug: "github", DisplayName: "GitHub"},
	}

	rr := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/access"), "", managedAppSession(7, "user", false))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var workspace contract.AppAccessWorkspace
	if err := json.Unmarshal(rr.Body.Bytes(), &workspace); err != nil {
		t.Fatalf("decode access workspace: %v; body: %s", err, rr.Body.String())
	}
	if len(workspace.Providers) != 2 || workspace.Providers[0].Slug != "disabled-idp" || workspace.Providers[0].DisplayName != "Disabled Invite-only Provider" || workspace.Providers[1].Slug != "github" || workspace.Providers[1].DisplayName != "GitHub" {
		t.Fatalf("provider choices = %#v, want descriptors for disabled and invite-only providers", workspace.Providers)
	}
}

func TestManagedRulePreviewReturnsExactCountPagesAndBoundCursorWithoutWrites(t *testing.T) {
	s, queries, auditCapture := newPolicyTestServer()
	queries.accounts[44] = db.GetAccountAccessFactsRow{ID: 44, Username: "charlie", DisplayName: "Charlie", HasPasskey: true}
	queries.accounts[45] = db.GetAccountAccessFactsRow{ID: 45, Username: "disabled", DisplayName: "Disabled", Disabled: true, HasPasskey: true}
	body := `{"version":1,"condition":{"fact":"login_method","method":"passkey"},"limit":1}`

	first := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), body, managedAppSession(7, "user", false))
	if first.Code != http.StatusOK {
		t.Fatalf("first preview status = %d, want 200; body: %s", first.Code, first.Body.String())
	}
	var firstPage contract.RulePreviewPageView
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first preview page: %v; body: %s", err, first.Body.String())
	}
	if firstPage.MatchedCount != 2 || len(firstPage.Items) != 1 || firstPage.Items[0].Account.Username != "alice" || !firstPage.Items[0].Matched || firstPage.NextCursor == "" {
		t.Fatalf("first preview page = %#v", firstPage)
	}
	if queries.activeFactsCalls != 1 || queries.mutationCalls != 0 || len(auditCapture.records) != 0 {
		t.Fatalf("preview side effects = active facts:%d mutations:%d audit:%d, want one snapshot and no writes", queries.activeFactsCalls, queries.mutationCalls, len(auditCapture.records))
	}

	secondBody := `{"version":1,"condition":{"fact":"login_method","method":"passkey"},"limit":1,"cursor":"` + firstPage.NextCursor + `"}`
	second := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), secondBody, managedAppSession(7, "user", false))
	if second.Code != http.StatusOK {
		t.Fatalf("second preview status = %d, want 200; body: %s", second.Code, second.Body.String())
	}
	var secondPage contract.RulePreviewPageView
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second preview page: %v; body: %s", err, second.Body.String())
	}
	if secondPage.MatchedCount != 2 || len(secondPage.Items) != 1 || secondPage.Items[0].Account.Username != "bob" || secondPage.Items[0].Matched || secondPage.NextCursor == "" {
		t.Fatalf("second preview page = %#v", secondPage)
	}

	changedRule := `{"version":1,"condition":{"fact":"login_method","method":"federation"},"limit":1,"cursor":"` + firstPage.NextCursor + `"}`
	assertManagedAPIError(t, managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), changedRule, managedAppSession(7, "user", false)), http.StatusBadRequest, "pagination_cursor_invalid")
	assertManagedAPIError(t, managedRequest(t, s, http.MethodPost, managedURL("forward_auth", "forward", "/rule-preview"), secondBody, managedAppSession(7, "user", false)), http.StatusBadRequest, "pagination_cursor_invalid")
	if queries.mutationCalls != 0 || len(auditCapture.records) != 0 {
		t.Fatalf("preview pagination wrote policy data: mutations:%d audit:%d", queries.mutationCalls, len(auditCapture.records))
	}
}

func TestManagedRulePreviewValidatesStrictRequestAndClosedRule(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	session := managedAppSession(7, "user", false)

	for name, body := range map[string]string{
		"unknown request field": `{"version":1,"condition":{"fact":"login_method","method":"passkey"},"unexpected":true}`,
		"trailing JSON":         `{"version":1,"condition":{"fact":"login_method","method":"passkey"}} {}`,
		"malformed JSON":        `{"version":1,"condition":`,
	} {
		t.Run(name, func(t *testing.T) {
			assertManagedAPIError(t, managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), body, session), http.StatusBadRequest, "bad_request")
		})
	}

	invalid := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), `{"version":1,"condition":{"fact":"connection.provider","provider":"unknown"}}`, session)
	assertRuleValidationError(t, invalid, "$.condition", "provider_not_found")

	unknownCondition := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), `{"version":1,"condition":{"fact":"login_method","method":"passkey","unexpected":true}}`, session)
	assertRuleValidationError(t, unknownCondition, "$", "unknown_field")
}

func TestManagedRulePreviewUsesExistingLimitBounds(t *testing.T) {
	s, queries, _ := newPolicyTestServer()
	for id := int32(100); id < 201; id++ {
		queries.accounts[id] = db.GetAccountAccessFactsRow{ID: id, Username: "user" + strconv.Itoa(int(id)), HasPasskey: true}
	}
	base := `{"version":1,"condition":{"fact":"login_method","method":"passkey"}`
	for name, body := range map[string]struct {
		body string
		want int
	}{
		"default": {body: base + `}`, want: 50},
		"clamped": {body: base + `,"limit":1000}`, want: 100},
	} {
		t.Run(name, func(t *testing.T) {
			rr := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), body.body, managedAppSession(7, "user", false))
			if rr.Code != http.StatusOK {
				t.Fatalf("preview status = %d, want 200; body: %s", rr.Code, rr.Body.String())
			}
			var page contract.RulePreviewPageView
			if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
				t.Fatalf("decode preview page: %v", err)
			}
			if len(page.Items) != body.want {
				t.Fatalf("items = %d, want %d", len(page.Items), body.want)
			}
		})
	}
}

func TestManagedRulePreviewUsesSharedJSONBodyControls(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	session := managedAppSession(7, "user", false)

	badType := httptest.NewRecorder()
	s.router.ServeHTTP(badType, reqWithSession(http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), `{"version":1,"condition":{"fact":"login_method","method":"passkey"}}`, "text/plain", session))
	assertManagedAPIError(t, badType, http.StatusBadRequest, "bad_request")

	oversized := `{"version":1,"condition":{"fact":"login_method","method":"passkey"},"cursor":"` + string(make([]byte, 64<<10)) + `"}`
	tooLarge := httptest.NewRecorder()
	s.router.ServeHTTP(tooLarge, reqWithSession(http.MethodPost, managedURL("oidc", "wiki", "/rule-preview"), oversized, "", session))
	assertManagedAPIError(t, tooLarge, http.StatusRequestEntityTooLarge, "request_too_large")
}
func TestManagedRouteInputIdentifiersRemainOpaque(t *testing.T) {
	for _, badID := range []string{"", "0", "-1", "not-a-number"} {
		t.Run(strconv.Quote(badID), func(t *testing.T) {
			s, _, _ := newPolicyTestServer()
			rr := managedRequest(t, s, http.MethodGet, managedURL("saml", badID, "/access"), "", managedAppSession(7, "user", false))
			if badID == "" {
				if rr.Code == http.StatusOK {
					t.Fatalf("empty identifier unexpectedly succeeded")
				}
				return
			}
			assertManagedAPIError(t, rr, http.StatusNotFound, "client_not_found")
		})
	}
}
