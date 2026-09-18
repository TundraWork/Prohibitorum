package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/pagination"
)

func (s *Server) registerGlobalGroupRoutes(router chiRouter) {
	const base = "/api/prohibitorum/groups"
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	sessionReq := contract.AuthRequirement{Kind: contract.AuthSession}
	registerOpHTTP(router, http.MethodGet, base, sessionReq, s.handleListGlobalGroupsHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, base, admin, s.handleCreateGlobalGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/providers", admin, s.handleListGlobalGroupProvidersHTTP)
	s.registerAdminBodyOpHTTP(router, http.MethodPost, base+"/rule-preview", admin, s.handlePreviewGlobalRuleHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{groupId}", admin, s.handleGetGlobalGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{groupId}/applications", admin, s.handleListGlobalGroupApplicationsHTTP)
	s.registerSudoOpHTTP(router, http.MethodPut, base+"/{groupId}", admin, s.handleUpdateGlobalGroupHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, base+"/{groupId}/delete", admin, s.handleDeleteGlobalGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{groupId}/decisions", admin, s.handleListGlobalGroupDecisionsHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, base+"/{groupId}/decisions", admin, s.handleUpsertGlobalGroupDecisionHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, base+"/{groupId}/decisions/clear", admin, s.handleClearGlobalGroupDecisionHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{groupId}/preview", admin, s.handlePreviewGlobalGroupHTTP)
	registerOpHTTP(router, http.MethodGet, base+"/{groupId}/explain/{accountId}", admin, s.handleExplainGlobalGroupHTTP)
}

func (s *Server) handleListGlobalGroupProvidersHTTP(w http.ResponseWriter, r *http.Request) {
	rows, err := s.appPolicyQ().ListKnownUpstreamIDPDescriptors(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list global group providers: %w", err))
		return
	}
	items := make([]contract.ProviderDescriptorView, len(rows))
	for i, row := range rows {
		items[i] = contract.ProviderDescriptorView{Slug: row.Slug, DisplayName: row.DisplayName}
	}
	writeJSON(w, items)
}

func (s *Server) handlePreviewGlobalRuleHTTP(w http.ResponseWriter, r *http.Request) {
	var body previewManagedRuleBody
	if err := decodePreviewManagedRuleBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	raw, err := json.Marshal(map[string]any{"version": body.Version, "condition": body.Condition})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("encode global rule preview: %w", err))
		return
	}
	canonical, err := s.canonicalRule(r.Context(), raw)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var rule appaccess.Rule
	if err := json.Unmarshal(canonical, &rule); err != nil {
		writeAuthErr(w, fmt.Errorf("decode global rule preview: %w", err))
		return
	}
	rows, err := s.appPolicyQ().ListActiveAccountAccessFacts(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("preview global rule: %w", err))
		return
	}
	items := make([]contract.GroupPreviewView, 0, len(rows))
	matched := 0
	for _, row := range rows {
		facts, err := appaccess.FactsFromActiveRow(row)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		result := appaccess.EvaluateCondition(rule.Condition, facts).Result
		if result {
			matched++
		}
		items = append(items, contract.GroupPreviewView{Account: contract.AccountSummaryView{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}, Matched: result})
	}
	limit := pagination.Limit(body.Limit)
	start := 0
	if body.Cursor != "" {
		payload, err := s.decodeCursor(body.Cursor, "global_rule_preview", "username", map[string]string{})
		if err != nil {
			writeCursorInvalidErr(w, err)
			return
		}
		after, id := decodeASCTextIntKey(payload.Keys)
		for start < len(items) && (items[start].Account.Username < after || (items[start].Account.Username == after && items[start].Account.ID <= id)) {
			start++
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		last := items[end-1].Account
		next = s.encodeNextCursor("global_rule_preview", "username", map[string]string{}, encodeASCTextIntKey(last.Username, last.ID))
	}
	writeJSON(w, contract.RulePreviewPageView{Items: items[start:end], MatchedCount: matched, NextCursor: next})
}

func (s *Server) requireGlobalGroupKind(ctx context.Context, groupID int32, kind string) (db.UserGroup, error) {
	group, err := s.appPolicyQ().GetGlobalGroup(ctx, groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.UserGroup{}, authn.ErrGroupNotFound()
	}
	if err != nil {
		return db.UserGroup{}, fmt.Errorf("get global group: %w", err)
	}
	if group.Kind != kind {
		return db.UserGroup{}, authn.ErrBadRequest()
	}
	return group, nil
}

func (s *Server) handleListGlobalGroupDecisionsHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireGlobalGroupKind(r.Context(), groupID, "manual"); err != nil {
		writeAuthErr(w, err)
		return
	}
	limit, err := appPolicyLimit(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	filters := map[string]string{"groupId": strconv.FormatInt(int64(groupID), 10)}
	payload, err := s.decodeCursor(r.URL.Query().Get("cursor"), "global_group_decisions", "username", filters)
	if err != nil {
		writeCursorInvalidErr(w, err)
		return
	}
	afterUsername, afterID := decodeASCTextIntKey(payload.Keys)
	rows, err := s.appPolicyQ().ListManualDecisionsPage(r.Context(), db.ListManualDecisionsPageParams{GroupID: groupID, AfterUsername: pgText(afterUsername), AfterAccountID: pgInt4(afterID), RowLimit: int32(limit + 1)})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list global group decisions: %w", err))
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]contract.ManualDecisionView, 0, len(rows))
	for _, row := range rows {
		items = append(items, contract.ManualDecisionView{Account: contract.AccountSummaryView{ID: row.AccountID, Username: row.Username, DisplayName: row.DisplayName}, Effect: row.Effect, UpdatedAt: row.UpdatedAt.Time})
	}
	next := ""
	if more {
		last := rows[len(rows)-1]
		next = s.encodeNextCursor("global_group_decisions", "username", filters, encodeASCTextIntKey(last.Username, last.AccountID))
	}
	writeJSON(w, buildPage(items, next))
}

func (s *Server) handleUpsertGlobalGroupDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireGlobalGroupKind(r.Context(), groupID, "manual"); err != nil {
		writeAuthErr(w, err)
		return
	}
	var body manualDecisionBody
	if err := decodeAppPolicyBody(r, &body); err != nil || (body.Effect != "allow" && body.Effect != "deny") {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	account, err := s.manualDecisionAccount(r.Context(), body.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	sess := authn.SessionFromContext(r.Context())
	decision, err := s.appPolicyQ().UpsertManualDecision(r.Context(), db.UpsertManualDecisionParams{GroupID: groupID, AccountID: body.AccountID, Effect: body.Effect, CreatedBy: pgtype.Int4{Int32: sess.Account.ID, Valid: true}})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("upsert global group decision: %w", err))
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: auditActor(sess), Factor: audit.FactorAppPolicy, Event: audit.EventUpdate, Detail: map[string]any{"group_id": groupID, "account_id": body.AccountID, "effect": body.Effect}})
	writeJSON(w, contract.ManualDecisionView{Account: account, Effect: decision.Effect, UpdatedAt: decision.UpdatedAt.Time})
}

func (s *Server) handleClearGlobalGroupDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireGlobalGroupKind(r.Context(), groupID, "manual"); err != nil {
		writeAuthErr(w, err)
		return
	}
	var body clearManualDecisionBody
	if err := decodeAppPolicyBody(r, &body); err != nil || body.AccountID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if _, err := s.appPolicyQ().ClearManualDecision(r.Context(), db.ClearManualDecisionParams{GroupID: groupID, AccountID: body.AccountID}); err != nil {
		writeAuthErr(w, fmt.Errorf("clear global group decision: %w", err))
		return
	}
	sess := authn.SessionFromContext(r.Context())
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: auditActor(sess), Factor: audit.FactorAppPolicy, Event: audit.EventRevoke, Detail: map[string]any{"group_id": groupID, "account_id": body.AccountID}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) globalRuleAndProviders(ctx context.Context, groupID int32) (appaccess.Rule, error) {
	group, err := s.requireGlobalGroupKind(ctx, groupID, "rule")
	if err != nil {
		return appaccess.Rule{}, err
	}
	providers, err := s.knownProviderSet(ctx)
	if err != nil {
		return appaccess.Rule{}, err
	}
	rule, err := appaccess.ParseAndValidateRule(group.Rule, providers)
	if err != nil {
		return appaccess.Rule{}, appRuleValidationErr(err)
	}
	return rule, nil
}

func (s *Server) handlePreviewGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	rule, err := s.globalRuleAndProviders(r.Context(), groupID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	limit, err := appPolicyLimit(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	rows, err := s.appPolicyQ().ListActiveAccountAccessFactsPage(r.Context(), db.ListActiveAccountAccessFactsPageParams{RowLimit: int32(limit)})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("preview global group: %w", err))
		return
	}
	items := make([]contract.GroupPreviewView, 0, len(rows))
	for _, row := range rows {
		facts, err := appaccess.FactsFromPageRow(row)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		items = append(items, contract.GroupPreviewView{Account: contract.AccountSummaryView{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}, Matched: appaccess.EvaluateCondition(rule.Condition, facts).Result})
	}
	writeJSON(w, buildPage(items, ""))
}

func (s *Server) handleExplainGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	rule, err := s.globalRuleAndProviders(r.Context(), groupID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	accountID, err := accountIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	row, err := s.appPolicyQ().GetAccountAccessFacts(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) || row.Disabled {
		writeAuthErr(w, authn.ErrAccountNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("load explanation account: %w", err))
		return
	}
	facts, err := appaccess.FactsFromRow(row)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, contract.GroupExplanationView{Account: accountSummaryFromFacts(row), Explanation: contractExplanation(appaccess.EvaluateCondition(rule.Condition, facts))})
}

func (s *Server) handleListGlobalGroupsHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Account == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var (
		groups []db.UserGroup
		views  []contract.AppGroupView
		err    error
	)
	if sess.Account.Role == "admin" {
		groups, err = s.appPolicyQ().ListGlobalGroups(r.Context())
		if err == nil {
			views, err = s.appGroupViews(r.Context(), groups)
		}
		if err == nil {
			var counts []db.ListGlobalGroupApplicationCountsRow
			counts, err = s.appPolicyQ().ListGlobalGroupApplicationCounts(r.Context())
			if err == nil {
				byGroup := make(map[int32]int64, len(counts))
				for _, count := range counts {
					byGroup[count.GroupID] = count.ApplicationCount
				}
				for i := range views {
					count := byGroup[views[i].ID]
					views[i].ApplicationCount = &count
				}
			}
		}
	} else {
		groups, err = s.appPolicyEvaluator().ListAccountGroups(r.Context(), sess.Account.ID)
		if err == nil {
			views = make([]contract.AppGroupView, 0, len(groups))
		}
		for _, group := range groups {
			view := contract.AppGroupView{ID: group.ID, Kind: group.Kind, Slug: group.Slug, DisplayName: group.DisplayName, ExposedToDownstream: group.ExposedToDownstream}
			if group.Description.Valid {
				view.Description = group.Description.String
			}
			views = append(views, view)
		}
	}
	if err != nil {
		writeAuthErr(w, appRuleValidationErr(err))
		return
	}
	writeJSON(w, views)
}

type globalGroupReferenceQueries interface {
	ListGlobalGroupApplications(context.Context, int32) ([]db.ListGlobalGroupApplicationsRow, error)
}

func (s *Server) handleListGlobalGroupApplicationsHTTP(w http.ResponseWriter, r *http.Request) {
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.appPolicyQ().GetGlobalGroup(r.Context(), groupID); errors.Is(err, pgx.ErrNoRows) {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	} else if err != nil {
		writeAuthErr(w, fmt.Errorf("get global group: %w", err))
		return
	}
	q, ok := s.appPolicyQ().(globalGroupReferenceQueries)
	if !ok {
		writeAuthErr(w, errors.New("global group application references unavailable"))
		return
	}
	rows, err := q.ListGlobalGroupApplications(r.Context(), groupID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list global group applications: %w", err))
		return
	}
	items := make([]contract.GroupApplicationView, 0, len(rows))
	for _, row := range rows {
		iconKind := "oidc_client"
		if row.Kind == "saml" {
			iconKind = "saml_sp"
		}
		etag, iconErr := s.appPolicyQ().GetEntityIconEtag(r.Context(), db.GetEntityIconEtagParams{OwnerKind: iconKind, OwnerID: row.AppID})
		if iconErr != nil && !errors.Is(iconErr, pgx.ErrNoRows) {
			writeAuthErr(w, fmt.Errorf("load global group application icon: %w", iconErr))
			return
		}
		items = append(items, contract.GroupApplicationView{IconURL: entityIconURLPtr(iconKind, row.AppID, etag), Kind: row.Kind, AppID: row.AppID, DisplayName: row.DisplayName})
	}
	writeJSON(w, buildPage(items, ""))
}

func (s *Server) handleCreateGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	var body createAppGroupBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	if validateAppGroupSlug(body.Slug) != nil || body.DisplayName == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	exposed := true
	if body.ExposedToDownstream != nil {
		exposed = *body.ExposedToDownstream
	}
	var rule []byte
	var err error
	switch body.Kind {
	case "manual":
		if isPresentJSON(body.Rule) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
	case "rule":
		rule, err = s.canonicalRule(r.Context(), body.Rule)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	group, err := s.appPolicyQ().CreateGlobalGroup(r.Context(), db.CreateGlobalGroupParams{Kind: body.Kind, Slug: body.Slug, DisplayName: body.DisplayName, Description: appGroupDescription(body.Description), ExposedToDownstream: exposed, Rule: rule})
	if err != nil {
		writeAuthErr(w, appGroupWriteErr(err))
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	sess := authn.SessionFromContext(r.Context())
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: auditActor(sess), Factor: audit.FactorAppPolicy, Event: audit.EventRegister, Detail: map[string]any{"group_id": group.ID, "group_kind": group.Kind}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, view)
}

func (s *Server) handleGetGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	group, err := s.appPolicyQ().GetGlobalGroup(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("get global group: %w", err))
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	sess := authn.SessionFromContext(r.Context())
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: auditActor(sess), Factor: audit.FactorAppPolicy, Event: audit.EventUpdate, Detail: map[string]any{"group_id": group.ID, "group_kind": group.Kind}})
	writeJSON(w, view)
}

func (s *Server) handleUpdateGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	current, err := s.appPolicyQ().GetGlobalGroup(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("get global group: %w", err))
		return
	}
	var body updateAppGroupBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	if body.Kind != nil || validateAppGroupSlug(body.Slug) != nil || body.DisplayName == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	exposed := current.ExposedToDownstream
	if body.ExposedToDownstream != nil {
		exposed = *body.ExposedToDownstream
	}
	rule := current.Rule
	if current.Kind == "manual" {
		if isPresentJSON(body.Rule) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		rule = nil
	} else if isPresentJSON(body.Rule) {
		rule, err = s.canonicalRule(r.Context(), body.Rule)
		if err != nil {
			writeAuthErr(w, err)
			return
		}
	}
	group, err := s.appPolicyQ().UpdateGlobalGroup(r.Context(), db.UpdateGlobalGroupParams{Slug: body.Slug, DisplayName: body.DisplayName, Description: appGroupDescription(body.Description), ExposedToDownstream: exposed, Rule: rule, GroupID: id})
	if err != nil {
		writeAuthErr(w, appGroupWriteErr(err))
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, view)
}

func (s *Server) handleDeleteGlobalGroupHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	rows, err := s.appPolicyQ().DeleteGlobalGroup(r.Context(), id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23001" {
		writeAuthErr(w, authn.ErrGroupInUse())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("delete global group: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	}
	sess := authn.SessionFromContext(r.Context())
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{AccountID: auditActor(sess), Factor: audit.FactorAppPolicy, Event: audit.EventRevoke, Detail: map[string]any{"group_id": id}})
	w.WriteHeader(http.StatusNoContent)
}
