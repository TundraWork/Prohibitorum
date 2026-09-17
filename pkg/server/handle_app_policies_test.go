package server

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

func TestManagedApplicationGroupSelectionIsAtomic(t *testing.T) {
	s, queries, _ := newPolicyTestServer()
	selected := managedRequest(t, s, http.MethodPut, managedURL("oidc", "wiki", "/groups"), `{"groupIds":[2,3]}`, managedAppSession(7, "app_manager", false))
	if selected.Code != http.StatusOK {
		t.Fatalf("select status = %d; body: %s", selected.Code, selected.Body.String())
	}
	if queries.oidcGroups["wiki"][1] || !queries.oidcGroups["wiki"][2] || !queries.oidcGroups["wiki"][3] {
		t.Fatalf("links = %#v", queries.oidcGroups["wiki"])
	}

	before := maps.Clone(queries.oidcGroups["wiki"])
	unknown := managedRequest(t, s, http.MethodPut, managedURL("oidc", "wiki", "/groups"), `{"groupIds":[2,999]}`, managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, unknown, http.StatusNotFound, "group_not_found")
	if !reflect.DeepEqual(queries.oidcGroups["wiki"], before) {
		t.Fatalf("failed replacement mutated links: %#v", queries.oidcGroups["wiki"])
	}

	duplicate := managedRequest(t, s, http.MethodPut, managedURL("oidc", "wiki", "/groups"), `{"groupIds":[2,2]}`, managedAppSession(7, "app_manager", false))
	assertManagedAPIError(t, duplicate, http.StatusBadRequest, "bad_request")
}

func assertRuleValidationError(t *testing.T, rr *httptest.ResponseRecorder, wantPath, wantReason string) {
	t.Helper()
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}
	var public struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.Code != "invalid_group_rule" || public.Details["path"] != wantPath || public.Details["reason"] != wantReason {
		t.Fatalf("validation error = %#v, want path=%s reason=%s", public, wantPath, wantReason)
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
