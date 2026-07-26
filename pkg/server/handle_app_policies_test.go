package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

func TestCreateSecondManualGroupConflicts(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups"),
		`{"kind":"manual","slug":"another-exception","displayName":"Another exception"}`, managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, rr, http.StatusConflict, "manual_group_exists")
}

func TestAppGroupSlugUniquenessIsScopedToBoundApplication(t *testing.T) {
	s, queries, _ := newPolicyTestServer()

	duplicate := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups"),
		`{"kind":"rule","slug":"passkeys","displayName":"Duplicate","rule":{"version":1,"condition":{"fact":"login_method","method":"passkey"}}}`, managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, duplicate, http.StatusConflict, "group_slug_conflict")

	reused := managedRequest(t, s, http.MethodPost, managedURL("saml", "7", "/groups"),
		`{"kind":"rule","slug":"passkeys","displayName":"SAML passkeys","rule":{"version":1,"condition":{"fact":"login_method","method":"passkey"}}}`, managedAppSession(99, "admin", false))
	if reused.Code != http.StatusCreated {
		t.Fatalf("reuse slug status = %d, want 201; body: %s", reused.Code, reused.Body.String())
	}
	var group contract.AppGroupView
	if err := json.Unmarshal(reused.Body.Bytes(), &group); err != nil {
		t.Fatalf("decode created SAML group: %v", err)
	}
	stored := queries.groups[group.ID]
	if !stored.SamlSpID.Valid || stored.SamlSpID.Int64 != 7 || stored.OidcClientID.Valid {
		t.Fatalf("SAML group binding = %#v, want only saml_sp_id=7", stored)
	}
}

func TestAppGroupUpdateCannotChangeAppBinding(t *testing.T) {
	s, queries, _ := newPolicyTestServer()
	before := queries.groups[1]

	rr := managedRequest(t, s, http.MethodPut, managedURL("oidc", "other", "/groups/1"),
		`{"slug":"moved","displayName":"Moved"}`, managedAppSession(99, "admin", false))
	assertManagedAPIError(t, rr, http.StatusNotFound, "group_not_found")

	after := queries.groups[1]
	if after.OidcClientID != before.OidcClientID || after.SamlSpID != before.SamlSpID || after.Slug != before.Slug {
		t.Fatalf("cross-app update mutated bound group: before=%#v after=%#v", before, after)
	}
}

func TestManualDecisionUpsertClearAndRuleRejection(t *testing.T) {
	s, _, auditCapture := newPolicyTestServer()

	allow := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups/1/decisions"),
		`{"accountId":42,"effect":"allow"}`, managedAppSession(7, "app_manager", false))
	if allow.Code != http.StatusOK {
		t.Fatalf("allow status = %d, want 200; body: %s", allow.Code, allow.Body.String())
	}
	var decision contract.ManualDecisionView
	if err := json.Unmarshal(allow.Body.Bytes(), &decision); err != nil {
		t.Fatalf("decode allow decision: %v", err)
	}
	if decision.Effect != "allow" || decision.Account.ID != 42 || decision.Account.Username != "alice" {
		t.Fatalf("allow decision = %#v", decision)
	}

	deny := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups/1/decisions"),
		`{"accountId":42,"effect":"deny"}`, managedAppSession(7, "app_manager", false))
	if deny.Code != http.StatusOK {
		t.Fatalf("deny status = %d, want 200; body: %s", deny.Code, deny.Body.String())
	}

	list := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/groups/1/decisions"), "", managedAppSession(7, "app_manager", false))
	if list.Code != http.StatusOK {
		t.Fatalf("decision list status = %d, want 200; body: %s", list.Code, list.Body.String())
	}
	var page contract.Page[contract.ManualDecisionView]
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode decision page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Effect != "deny" {
		t.Fatalf("decision page = %#v", page)
	}

	clear := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups/1/decisions/clear"),
		`{"accountId":42}`, managedAppSession(7, "app_manager", false))
	if clear.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204; body: %s", clear.Code, clear.Body.String())
	}

	ruleDecision := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups/2/decisions"),
		`{"accountId":42,"effect":"allow"}`, managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, ruleDecision, http.StatusBadRequest, "bad_request")

	if len(auditCapture.records) != 3 {
		t.Fatalf("manual-decision audit record count = %d, want 3", len(auditCapture.records))
	}
	for _, record := range auditCapture.records {
		if record.Factor != audit.FactorAppPolicy {
			t.Fatalf("manual-decision audit factor = %q, want %q", record.Factor, audit.FactorAppPolicy)
		}
	}
}

func TestRuleValidationReturnsSafePathAndReason(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/groups"),
		`{"kind":"rule","slug":"unknown-provider","displayName":"Unknown provider","rule":{"version":1,"condition":{"fact":"connection.provider","provider":"unknown"}}}`, managedAppSession(7, "app_manager", false))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}
	var public struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &public); err != nil {
		t.Fatalf("decode invalid-rule response: %v", err)
	}
	if public.Code != "invalid_group_rule" {
		t.Fatalf("code = %q, want invalid_group_rule", public.Code)
	}
	if public.Details["path"] != "$.condition" || public.Details["reason"] != "provider_not_found" {
		t.Fatalf("safe validation details = %#v", public.Details)
	}
}

func TestRulePreviewPaginatesSafeAccountProjection(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/groups/2/preview?limit=1"), "", managedAppSession(7, "app_manager", false))
	if rr.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var page contract.Page[contract.GroupPreviewView]
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode preview page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Account.ID != 42 || !page.Items[0].Matched || page.NextCursor == "" {
		t.Fatalf("preview page = %#v", page)
	}
	if got := rr.Body.String(); containsAny(got, "confirmedProviderSlugs", "confirmedProtocols", "hasPasskey", "hasFederation") {
		t.Fatalf("preview exposed evaluator facts: %s", got)
	}

	next := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/groups/2/preview?limit=1&cursor="+page.NextCursor), "", managedAppSession(7, "app_manager", false))
	if next.Code != http.StatusOK {
		t.Fatalf("second preview status = %d, want 200; body: %s", next.Code, next.Body.String())
	}
	var nextPage contract.Page[contract.GroupPreviewView]
	if err := json.Unmarshal(next.Body.Bytes(), &nextPage); err != nil {
		t.Fatalf("decode second preview page: %v", err)
	}
	if len(nextPage.Items) != 1 || nextPage.Items[0].Account.ID != 43 || nextPage.Items[0].Matched {
		t.Fatalf("second preview page = %#v", nextPage)
	}
}

func TestRuleExplanationIsBoundedAndSafe(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/groups/2/explain/42"), "", managedAppSession(7, "app_manager", false))
	if rr.Code != http.StatusOK {
		t.Fatalf("explain status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var view contract.GroupExplanationView
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if view.Account.ID != 42 || view.Account.Username != "alice" || !view.Explanation.Result || view.Explanation.Path != "$" {
		t.Fatalf("explanation view = %#v", view)
	}
	if got := rr.Body.String(); containsAny(got, "confirmedProviderSlugs", "confirmedProtocols", "hasPasskey", "hasFederation", "password") {
		t.Fatalf("explanation exposed private facts: %s", got)
	}
	if countExplanationNodes(view.Explanation) > 64 {
		t.Fatalf("explanation exceeds rule node bound: %#v", view.Explanation)
	}
}

func TestRuleExplanationForDisabledAccountIsNotFound(t *testing.T) {
	s, queries, _ := newPolicyTestServer()
	queries.accounts[44] = db.GetAccountAccessFactsRow{ID: 44, Username: "disabled", DisplayName: "Disabled", Disabled: true}

	rr := managedRequest(t, s, http.MethodGet, managedURL("oidc", "wiki", "/groups/2/explain/44"), "", managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, rr, http.StatusNotFound, "account_not_found")
}

func TestAccessRestrictionToggleIsAuditedWithoutSudo(t *testing.T) {
	s, queries, auditCapture := newPolicyTestServer()
	rr := managedRequest(t, s, http.MethodPost, managedURL("oidc", "wiki", "/access/set-restricted"),
		`{"restricted":true}`, managedAppSession(7, "app_manager", false))
	if rr.Code != http.StatusOK {
		t.Fatalf("set restricted status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	if !queries.oidc["wiki"].AccessRestricted {
		t.Fatal("access restriction was not persisted")
	}
	if len(auditCapture.records) != 1 {
		t.Fatalf("audit records = %#v, want one", auditCapture.records)
	}
	record := auditCapture.records[0]
	if record.Factor != audit.FactorAppPolicy || record.Event != audit.EventAccessRestrictedSet || record.Detail["app_id"] != "wiki" || record.Detail["restricted"] != true {
		t.Fatalf("restriction audit record = %#v", record)
	}
}

func TestManagedPolicyMutationsUseSharedJSONControlsWithoutSudo(t *testing.T) {
	s, _, _ := newPolicyTestServer()
	badType := reqWithSession(http.MethodPost, managedURL("oidc", "wiki", "/access/set-restricted"), `{"restricted":true}`, "text/plain", managedAppSession(7, "app_manager", false))
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, badType)
	assertManagedAPIError(t, rr, http.StatusBadRequest, "bad_request")
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if len(fragment) > 0 && stringContains(value, fragment) {
			return true
		}
	}
	return false
}

func stringContains(value, fragment string) bool {
	for start := 0; start+len(fragment) <= len(value); start++ {
		if value[start:start+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func countExplanationNodes(explanation contract.ExplanationView) int {
	count := 1
	for _, child := range explanation.Children {
		count += countExplanationNodes(child)
	}
	return count
}
