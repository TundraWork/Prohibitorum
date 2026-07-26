package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

var appGroupSlugRe = regexp.MustCompile(`^[a-z0-9](-?[a-z0-9])*$`)

func validateAppGroupSlug(slug string) error {
	if slug == "" || len(slug) > 64 || !appGroupSlugRe.MatchString(slug) {
		return authn.ErrBadRequest()
	}
	return nil
}

func appGroupIDFromRequest(r *http.Request) (int32, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "groupId"), 10, 32)
	if err != nil || id <= 0 {
		return 0, authn.ErrGroupNotFound()
	}
	return int32(id), nil
}

func accountIDFromRequest(r *http.Request) (int32, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "accountId"), 10, 32)
	if err != nil || id <= 0 {
		return 0, authn.ErrAccountNotFound()
	}
	return int32(id), nil
}

func (s *Server) listBoundAppGroups(ctx context.Context, ref appaccess.AppRef) ([]db.UserGroup, error) {
	var (
		groups []db.UserGroup
		err    error
	)
	switch ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		groups, err = s.appPolicyQ().ListOIDCAppGroups(ctx, ref.OIDCClientID)
	case appaccess.KindSAML:
		groups, err = s.appPolicyQ().ListSAMLAppGroups(ctx, ref.SAMLSPID)
	default:
		return nil, authn.ErrClientNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("list app-bound groups: %w", err)
	}
	return groups, nil
}

func (s *Server) getBoundAppGroup(ctx context.Context, ref appaccess.AppRef, groupID int32) (db.UserGroup, error) {
	var (
		group db.UserGroup
		err   error
	)
	switch ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		group, err = s.appPolicyQ().GetOIDCAppGroup(ctx, db.GetOIDCAppGroupParams{GroupID: groupID, OidcClientID: ref.OIDCClientID})
	case appaccess.KindSAML:
		group, err = s.appPolicyQ().GetSAMLAppGroup(ctx, db.GetSAMLAppGroupParams{GroupID: groupID, SamlSpID: ref.SAMLSPID})
	default:
		return db.UserGroup{}, authn.ErrClientNotFound()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.UserGroup{}, authn.ErrGroupNotFound()
	}
	if err != nil {
		return db.UserGroup{}, fmt.Errorf("get app-bound group: %w", err)
	}
	return group, nil
}

func (s *Server) appGroupViews(ctx context.Context, groups []db.UserGroup) ([]contract.AppGroupView, error) {
	return s.appGroupViewsWithProviders(ctx, groups, nil)
}

func (s *Server) appGroupViewsWithProviders(ctx context.Context, groups []db.UserGroup, providers map[string]struct{}) ([]contract.AppGroupView, error) {
	views := make([]contract.AppGroupView, 0, len(groups))
	for _, group := range groups {
		view := contract.AppGroupView{
			ID: group.ID, Kind: group.Kind, Slug: group.Slug, DisplayName: group.DisplayName,
			ExposedToDownstream: group.ExposedToDownstream,
		}
		if group.Description.Valid {
			view.Description = group.Description.String
		}
		switch group.Kind {
		case "manual":
			if len(group.Rule) != 0 {
				return nil, authn.ErrInvalidGroupRule("$", "manual_group_has_rule")
			}
		case "rule":
			if providers == nil {
				var err error
				providers, err = s.knownProviderSet(ctx)
				if err != nil {
					return nil, err
				}
			}
			rule, err := appaccess.ParseAndValidateRule(group.Rule, providers)
			if err != nil {
				return nil, appRuleValidationErr(err)
			}
			converted := contractRule(rule)
			view.Rule = &converted
		default:
			return nil, authn.ErrBadRequest()
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Server) appGroupView(ctx context.Context, group db.UserGroup) (contract.AppGroupView, error) {
	views, err := s.appGroupViews(ctx, []db.UserGroup{group})
	if err != nil {
		return contract.AppGroupView{}, err
	}
	return views[0], nil
}

func (s *Server) knownProviderSlugs(ctx context.Context) ([]string, error) {
	slugs, err := s.appPolicyQ().ListKnownUpstreamIDPSlugs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list known upstream providers: %w", err)
	}
	return slugs, nil
}

func providerSet(slugs []string) map[string]struct{} {
	providers := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		providers[slug] = struct{}{}
	}
	return providers
}

func (s *Server) knownProviderSet(ctx context.Context) (map[string]struct{}, error) {
	slugs, err := s.knownProviderSlugs(ctx)
	if err != nil {
		return nil, err
	}
	return providerSet(slugs), nil
}

func contractRule(rule appaccess.Rule) contract.AppAccessRule {
	return contract.AppAccessRule{Version: rule.Version, Condition: contractCondition(rule.Condition)}
}

func contractCondition(condition appaccess.Condition) contract.AppAccessCondition {
	out := contract.AppAccessCondition{
		Op: condition.Op, Fact: condition.Fact, Provider: condition.Provider, Protocol: condition.Protocol,
		Method: condition.Method, Source: condition.Source,
	}
	if len(condition.Children) > 0 {
		out.Children = make([]contract.AppAccessCondition, len(condition.Children))
		for i := range condition.Children {
			out.Children[i] = contractCondition(condition.Children[i])
		}
	}
	if condition.Child != nil {
		child := contractCondition(*condition.Child)
		out.Child = &child
	}
	return out
}

func contractExplanation(explanation appaccess.Explanation) contract.ExplanationView {
	out := contract.ExplanationView{Path: explanation.Path, Label: explanation.Label, Result: explanation.Result}
	if len(explanation.Children) > 0 {
		out.Children = make([]contract.ExplanationView, len(explanation.Children))
		for i := range explanation.Children {
			out.Children[i] = contractExplanation(explanation.Children[i])
		}
	}
	return out
}

func appRuleValidationErr(err error) error {
	var ruleErr *appaccess.RuleError
	if errors.As(err, &ruleErr) {
		return authn.ErrInvalidGroupRule(ruleErr.Path, ruleErr.Reason)
	}
	if errors.Is(err, appaccess.ErrInvalidPolicy) {
		return authn.ErrInvalidGroupRule("$", "invalid_persisted_rule")
	}
	return fmt.Errorf("validate application group rule: %w", err)
}

func (s *Server) canonicalRule(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, authn.ErrInvalidGroupRule("$", "missing_rule")
	}
	providers, err := s.knownProviderSet(ctx)
	if err != nil {
		return nil, err
	}
	rule, err := appaccess.ParseAndValidateRule(raw, providers)
	if err != nil {
		return nil, appRuleValidationErr(err)
	}
	canonical, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("encode validated application group rule: %w", err)
	}
	return canonical, nil
}

func isPresentJSON(raw json.RawMessage) bool {
	return len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func appGroupWriteErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return authn.ErrGroupNotFound()
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "user_group_oidc_manual_uq", "user_group_saml_manual_uq":
			return authn.ErrManualGroupExists()
		case "user_group_oidc_slug_uq", "user_group_saml_slug_uq":
			return authn.ErrGroupSlugConflict()
		}
		return authn.ErrGroupSlugConflict()
	}
	return fmt.Errorf("write app-bound group: %w", err)
}

func (s *Server) recordAppPolicy(ctx context.Context, ref appaccess.AppRef, event string, detail map[string]any) {
	if detail == nil {
		detail = make(map[string]any, 2)
	}
	detail["app_kind"] = string(ref.Kind)
	if ref.Kind == appaccess.KindSAML {
		detail["app_id"] = strconv.FormatInt(ref.SAMLSPID, 10)
	} else {
		detail["app_id"] = ref.OIDCClientID
	}
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: auditActor(authn.SessionFromContext(ctx)),
		Factor:    audit.FactorAppPolicy,
		Event:     event,
		Detail:    detail,
	})
}

type createAppGroupBody struct {
	Kind                string          `json:"kind"`
	Slug                string          `json:"slug"`
	DisplayName         string          `json:"displayName"`
	Description         string          `json:"description"`
	ExposedToDownstream *bool           `json:"exposedToDownstream"`
	Rule                json.RawMessage `json:"rule"`
}

type updateAppGroupBody struct {
	Kind                *string         `json:"kind"`
	Slug                string          `json:"slug"`
	DisplayName         string          `json:"displayName"`
	Description         string          `json:"description"`
	ExposedToDownstream *bool           `json:"exposedToDownstream"`
	Rule                json.RawMessage `json:"rule"`
}

func decodeAppPolicyBody(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return authn.ErrBadRequest()
	}
	return nil
}

func appGroupDescription(description string) pgtype.Text {
	if description == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: description, Valid: true}
}

func (s *Server) handleCreateManagedApplicationGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var body createAppGroupBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := validateAppGroupSlug(body.Slug); err != nil || body.DisplayName == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	exposed := true
	if body.ExposedToDownstream != nil {
		exposed = *body.ExposedToDownstream
	}
	var rule []byte
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

	q := s.appPolicyQ()
	var group db.UserGroup
	switch app.ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		group, err = q.CreateOIDCAppGroup(r.Context(), db.CreateOIDCAppGroupParams{Kind: body.Kind, Slug: body.Slug, DisplayName: body.DisplayName, Description: appGroupDescription(body.Description), ExposedToDownstream: exposed, Rule: rule, OidcClientID: app.ref.OIDCClientID})
	case appaccess.KindSAML:
		group, err = q.CreateSAMLAppGroup(r.Context(), db.CreateSAMLAppGroupParams{Kind: body.Kind, Slug: body.Slug, DisplayName: body.DisplayName, Description: appGroupDescription(body.Description), ExposedToDownstream: exposed, Rule: rule, SamlSpID: app.ref.SAMLSPID})
	default:
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, appGroupWriteErr(err))
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	s.recordAppPolicy(r.Context(), app.ref, audit.EventRegister, map[string]any{"group_id": group.ID, "group_kind": group.Kind, "slug": group.Slug})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(view)
}

func (s *Server) handleGetManagedApplicationGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	group, err := s.getBoundAppGroup(r.Context(), app.ref, groupID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, view)
}

func (s *Server) handleUpdateManagedApplicationGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	current, err := s.getBoundAppGroup(r.Context(), app.ref, groupID)
	if err != nil {
		writeAuthErr(w, err)
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
	switch current.Kind {
	case "manual":
		if isPresentJSON(body.Rule) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		rule = nil
	case "rule":
		if isPresentJSON(body.Rule) {
			rule, err = s.canonicalRule(r.Context(), body.Rule)
			if err != nil {
				writeAuthErr(w, err)
				return
			}
		}
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	arg := db.UpdateAppGroupParams{Slug: body.Slug, DisplayName: body.DisplayName, Description: appGroupDescription(body.Description), ExposedToDownstream: exposed, Rule: rule, GroupID: groupID}
	if app.ref.Kind == appaccess.KindSAML {
		arg.SamlSpID = pgtype.Int8{Int64: app.ref.SAMLSPID, Valid: true}
	} else {
		arg.OidcClientID = pgtype.Text{String: app.ref.OIDCClientID, Valid: true}
	}
	group, err := s.appPolicyQ().UpdateAppGroup(r.Context(), arg)
	if err != nil {
		writeAuthErr(w, appGroupWriteErr(err))
		return
	}
	view, err := s.appGroupView(r.Context(), group)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	s.recordAppPolicy(r.Context(), app.ref, audit.EventUpdate, map[string]any{"group_id": group.ID, "group_kind": group.Kind, "slug": group.Slug})
	writeJSON(w, view)
}

func (s *Server) handleDeleteManagedApplicationGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	group, err := s.getBoundAppGroup(r.Context(), app.ref, groupID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var rows int64
	switch app.ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		rows, err = s.appPolicyQ().DeleteOIDCAppGroup(r.Context(), db.DeleteOIDCAppGroupParams{GroupID: groupID, OidcClientID: app.ref.OIDCClientID})
	case appaccess.KindSAML:
		rows, err = s.appPolicyQ().DeleteSAMLAppGroup(r.Context(), db.DeleteSAMLAppGroupParams{GroupID: groupID, SamlSpID: app.ref.SAMLSPID})
	default:
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("delete app-bound group: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	}
	s.recordAppPolicy(r.Context(), app.ref, audit.EventRevoke, map[string]any{"group_id": group.ID, "group_kind": group.Kind, "slug": group.Slug})
	w.WriteHeader(http.StatusNoContent)
}

type setManagedApplicationRestrictedBody struct {
	Restricted bool `json:"restricted"`
}

func (s *Server) handleSetManagedApplicationRestrictedHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	var body setManagedApplicationRestrictedBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	switch app.ref.Kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		_, err = s.appPolicyQ().SetOIDCClientAccessRestricted(r.Context(), db.SetOIDCClientAccessRestrictedParams{ClientID: app.ref.OIDCClientID, AccessRestricted: body.Restricted})
	case appaccess.KindSAML:
		_, err = s.appPolicyQ().SetSAMLSPAccessRestricted(r.Context(), db.SetSAMLSPAccessRestrictedParams{SamlSpID: app.ref.SAMLSPID, AccessRestricted: body.Restricted})
	default:
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
	if err != nil {
		writeAuthErr(w, fmt.Errorf("set managed application access restriction: %w", err))
		return
	}
	app.summary.AccessRestricted = body.Restricted
	s.recordAppPolicy(r.Context(), app.ref, audit.EventAccessRestrictedSet, map[string]any{"restricted": body.Restricted})
	writeJSON(w, app.summary)
}

func (s *Server) requireManualAppGroup(ctx context.Context, ref appaccess.AppRef, groupID int32) (db.UserGroup, error) {
	group, err := s.getBoundAppGroup(ctx, ref, groupID)
	if err != nil {
		return db.UserGroup{}, err
	}
	if group.Kind != "manual" {
		return db.UserGroup{}, authn.ErrBadRequest()
	}
	return group, nil
}

func (s *Server) requireRuleAppGroup(ctx context.Context, ref appaccess.AppRef, groupID int32) (db.UserGroup, error) {
	group, err := s.getBoundAppGroup(ctx, ref, groupID)
	if err != nil {
		return db.UserGroup{}, err
	}
	if group.Kind != "rule" {
		return db.UserGroup{}, authn.ErrBadRequest()
	}
	return group, nil
}

func (s *Server) handleListManagedGroupDecisionsHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireManualAppGroup(r.Context(), app.ref, groupID); err != nil {
		writeAuthErr(w, err)
		return
	}
	limit, err := appPolicyLimit(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	const collection = "managed_group_decisions"
	const sortID = "username"
	filters := managedAppCursorFilters(app.ref)
	filters["groupId"] = strconv.FormatInt(int64(groupID), 10)
	payload, err := s.decodeCursor(r.URL.Query().Get("cursor"), collection, sortID, filters)
	if err != nil {
		writeCursorInvalidErr(w, err)
		return
	}
	afterUsername, afterAccountID := decodeASCTextIntKey(payload.Keys)
	rows, err := s.appPolicyQ().ListManualDecisionsPage(r.Context(), db.ListManualDecisionsPageParams{GroupID: groupID, AfterUsername: pgText(afterUsername), AfterAccountID: pgInt4(afterAccountID), RowLimit: int32(limit + 1)})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list manual group decisions: %w", err))
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
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.Username, last.AccountID))
	}
	writeJSON(w, buildPage(items, next))
}

type manualDecisionBody struct {
	AccountID int32  `json:"accountId"`
	Effect    string `json:"effect"`
}

func (s *Server) manualDecisionAccount(ctx context.Context, accountID int32) (contract.AccountSummaryView, error) {
	if accountID <= 0 {
		return contract.AccountSummaryView{}, authn.ErrAccountNotFound()
	}
	account, err := s.appPolicyQ().GetAccountAccessFacts(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return contract.AccountSummaryView{}, authn.ErrAccountNotFound()
	}
	if err != nil {
		return contract.AccountSummaryView{}, fmt.Errorf("load manual-decision account: %w", err)
	}
	return accountSummaryFromFacts(account), nil
}

func (s *Server) handleUpsertManagedGroupDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireManualAppGroup(r.Context(), app.ref, groupID); err != nil {
		writeAuthErr(w, err)
		return
	}
	var body manualDecisionBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	if body.Effect != "allow" && body.Effect != "deny" {
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
		writeAuthErr(w, fmt.Errorf("upsert manual group decision: %w", err))
		return
	}
	event := audit.EventAccessGranted
	if body.Effect == "deny" {
		event = audit.EventAccessDenied
	}
	s.recordAppPolicy(r.Context(), app.ref, event, map[string]any{"group_id": groupID, "account_id": body.AccountID, "effect": body.Effect})
	writeJSON(w, contract.ManualDecisionView{Account: account, Effect: decision.Effect, UpdatedAt: decision.UpdatedAt.Time})
}

type clearManualDecisionBody struct {
	AccountID int32 `json:"accountId"`
}

func (s *Server) handleClearManagedGroupDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireManualAppGroup(r.Context(), app.ref, groupID); err != nil {
		writeAuthErr(w, err)
		return
	}
	var body clearManualDecisionBody
	if err := decodeAppPolicyBody(r, &body); err != nil {
		writeAuthErr(w, err)
		return
	}
	if body.AccountID <= 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	rows, err := s.appPolicyQ().ClearManualDecision(r.Context(), db.ClearManualDecisionParams{GroupID: groupID, AccountID: body.AccountID})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("clear manual group decision: %w", err))
		return
	}
	if rows > 0 {
		s.recordAppPolicy(r.Context(), app.ref, audit.EventAccessRevoked, map[string]any{"group_id": groupID, "account_id": body.AccountID})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewManagedGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireRuleAppGroup(r.Context(), app.ref, groupID); err != nil {
		writeAuthErr(w, err)
		return
	}
	limit, err := appPolicyLimit(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	const collection = "managed_group_preview"
	const sortID = "username"
	filters := managedAppCursorFilters(app.ref)
	filters["groupId"] = strconv.FormatInt(int64(groupID), 10)
	payload, err := s.decodeCursor(r.URL.Query().Get("cursor"), collection, sortID, filters)
	if err != nil {
		writeCursorInvalidErr(w, err)
		return
	}
	afterUsername, afterAccountID := decodeASCTextIntKey(payload.Keys)
	previews, err := s.appPolicyEvaluator().PreviewGroup(r.Context(), app.ref, groupID, db.ListActiveAccountAccessFactsPageParams{AfterUsername: pgText(afterUsername), AfterAccountID: pgInt4(afterAccountID), RowLimit: int32(limit + 1)})
	if err != nil {
		writeAuthErr(w, appPolicyReadErr(err))
		return
	}
	more := len(previews) > limit
	if more {
		previews = previews[:limit]
	}
	items := make([]contract.GroupPreviewView, 0, len(previews))
	for _, preview := range previews {
		items = append(items, contract.GroupPreviewView{Account: contract.AccountSummaryView{ID: preview.Account.ID, Username: preview.Account.Username, DisplayName: preview.Account.DisplayName}, Matched: preview.Matched})
	}
	next := ""
	if more && len(previews) > 0 {
		last := previews[len(previews)-1]
		next = s.encodeNextCursor(collection, sortID, filters, encodeASCTextIntKey(last.Account.Username, last.Account.ID))
	}
	writeJSON(w, buildPage(items, next))
}

func (s *Server) handleExplainManagedGroupHTTP(w http.ResponseWriter, r *http.Request) {
	app, err := s.managedApplicationFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	groupID, err := appGroupIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if _, err := s.requireRuleAppGroup(r.Context(), app.ref, groupID); err != nil {
		writeAuthErr(w, err)
		return
	}
	accountID, err := accountIDFromRequest(r)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	account, err := s.manualDecisionAccount(r.Context(), accountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	explanation, err := s.appPolicyEvaluator().ExplainGroup(r.Context(), app.ref, groupID, accountID)
	if err != nil {
		writeAuthErr(w, appPolicyReadErr(err))
		return
	}
	writeJSON(w, contract.GroupExplanationView{Account: account, Explanation: contractExplanation(explanation)})
}

func appPolicyReadErr(err error) error {
	if errors.Is(err, appaccess.ErrAppNotFound) {
		return authn.ErrClientNotFound()
	}
	if errors.Is(err, appaccess.ErrAccountDisabled) {
		return authn.ErrAccountNotFound()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return authn.ErrGroupNotFound()
	}
	if mapped := appRuleValidationErr(err); authn.AsAuthError(mapped) != nil {
		return mapped
	}
	return fmt.Errorf("read app policy: %w", err)
}
