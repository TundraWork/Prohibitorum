package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

type managerAuditCapture struct{ records []audit.Record }

func (c *managerAuditCapture) Record(_ context.Context, record audit.Record) error {
	c.records = append(c.records, record)
	return nil
}

type fakeManagerAssignmentQueries struct {
	oidcClients    map[string]db.OidcClient
	samlSPs        map[int64]db.SamlSp
	accounts       map[int32]db.Account
	oidcManagers   map[string]map[int32]time.Time
	samlManagers   map[int64]map[int32]time.Time
	candidates     []db.ListActiveAppManagerCandidatesRow
	candidateQuery string

	assignOIDCCalls  int
	assignSAMLCalls  int
	removeOIDCCalls  int
	removeSAMLCalls  int
	lockAccountCalls int
	beginCalls       int
	commitCalls      int
	rollbackCalls    int
	callOrder        []string
	assignOIDCErr    error
}

func (q *fakeManagerAssignmentQueries) ListActiveAppManagerCandidates(_ context.Context, query string) ([]db.ListActiveAppManagerCandidatesRow, error) {
	q.candidateQuery = query
	return q.candidates, nil
}

func newFakeManagerAssignmentQueries() *fakeManagerAssignmentQueries {
	return &fakeManagerAssignmentQueries{
		oidcClients:  make(map[string]db.OidcClient),
		samlSPs:      make(map[int64]db.SamlSp),
		accounts:     make(map[int32]db.Account),
		oidcManagers: make(map[string]map[int32]time.Time),
		samlManagers: make(map[int64]map[int32]time.Time),
	}
}

func (q *fakeManagerAssignmentQueries) GetOIDCClientAny(_ context.Context, clientID string) (db.OidcClient, error) {
	client, ok := q.oidcClients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return client, nil
}

func (q *fakeManagerAssignmentQueries) GetSAMLSPByID(_ context.Context, id int64) (db.SamlSp, error) {
	sp, ok := q.samlSPs[id]
	if !ok {
		return db.SamlSp{}, pgx.ErrNoRows
	}
	return sp, nil
}

func (q *fakeManagerAssignmentQueries) GetAccountByIDForUpdate(_ context.Context, id int32) (db.Account, error) {
	q.lockAccountCalls++
	q.callOrder = append(q.callOrder, "lock")
	account, ok := q.accounts[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return account, nil
}

func managerIDs(assignments map[int32]time.Time) []int32 {
	ids := make([]int32, 0, len(assignments))
	for id := range assignments {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (q *fakeManagerAssignmentQueries) ListOIDCClientManagers(_ context.Context, clientID string) ([]db.ListOIDCClientManagersRow, error) {
	assignments := q.oidcManagers[clientID]
	rows := make([]db.ListOIDCClientManagersRow, 0, len(assignments))
	for _, accountID := range managerIDs(assignments) {
		account := q.accounts[accountID]
		rows = append(rows, db.ListOIDCClientManagersRow{
			ClientID: clientID, AccountID: accountID,
			CreatedAt: pgtype.Timestamptz{Time: assignments[accountID], Valid: true},
			Username:  account.Username, DisplayName: account.DisplayName,
			Role: account.Role, Disabled: account.Disabled,
		})
	}
	return rows, nil
}

func (q *fakeManagerAssignmentQueries) ListSAMLSPManagers(_ context.Context, samlSPID int64) ([]db.ListSAMLSPManagersRow, error) {
	assignments := q.samlManagers[samlSPID]
	rows := make([]db.ListSAMLSPManagersRow, 0, len(assignments))
	for _, accountID := range managerIDs(assignments) {
		account := q.accounts[accountID]
		rows = append(rows, db.ListSAMLSPManagersRow{
			SamlSpID: samlSPID, AccountID: accountID,
			CreatedAt: pgtype.Timestamptz{Time: assignments[accountID], Valid: true},
			Username:  account.Username, DisplayName: account.DisplayName,
			Role: account.Role, Disabled: account.Disabled,
		})
	}
	return rows, nil
}

func (q *fakeManagerAssignmentQueries) AssignOIDCClientManager(_ context.Context, arg db.AssignOIDCClientManagerParams) error {
	q.assignOIDCCalls++
	q.callOrder = append(q.callOrder, "assign")
	if q.assignOIDCErr != nil {
		return q.assignOIDCErr
	}
	if q.oidcManagers[arg.ClientID] == nil {
		q.oidcManagers[arg.ClientID] = make(map[int32]time.Time)
	}
	if _, exists := q.oidcManagers[arg.ClientID][arg.AccountID]; !exists {
		q.oidcManagers[arg.ClientID][arg.AccountID] = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	}
	return nil
}

func (q *fakeManagerAssignmentQueries) AssignSAMLSPManager(_ context.Context, arg db.AssignSAMLSPManagerParams) error {
	q.assignSAMLCalls++
	if q.samlManagers[arg.SamlSpID] == nil {
		q.samlManagers[arg.SamlSpID] = make(map[int32]time.Time)
	}
	if _, exists := q.samlManagers[arg.SamlSpID][arg.AccountID]; !exists {
		q.samlManagers[arg.SamlSpID][arg.AccountID] = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	}
	return nil
}

func (q *fakeManagerAssignmentQueries) RemoveOIDCClientManager(_ context.Context, arg db.RemoveOIDCClientManagerParams) (int64, error) {
	q.removeOIDCCalls++
	if _, exists := q.oidcManagers[arg.ClientID][arg.AccountID]; !exists {
		return 0, nil
	}
	delete(q.oidcManagers[arg.ClientID], arg.AccountID)
	return 1, nil
}

func (q *fakeManagerAssignmentQueries) RemoveSAMLSPManager(_ context.Context, arg db.RemoveSAMLSPManagerParams) (int64, error) {
	q.removeSAMLCalls++
	if _, exists := q.samlManagers[arg.SamlSpID][arg.AccountID]; !exists {
		return 0, nil
	}
	delete(q.samlManagers[arg.SamlSpID], arg.AccountID)
	return 1, nil
}

type managerAssignmentTestTx struct {
	queries   *fakeManagerAssignmentQueries
	committed bool
}

func (tx *managerAssignmentTestTx) Queries() managerAssignmentQueries { return tx.queries }

func (tx *managerAssignmentTestTx) Commit(context.Context) error {
	tx.committed = true
	tx.queries.commitCalls++
	tx.queries.callOrder = append(tx.queries.callOrder, "commit")
	return nil
}

func (tx *managerAssignmentTestTx) Rollback(context.Context) error {
	if !tx.committed {
		tx.queries.rollbackCalls++
		tx.queries.callOrder = append(tx.queries.callOrder, "rollback")
	}
	return nil
}

type managerAssignmentTestRunner struct{ queries *fakeManagerAssignmentQueries }

func (r managerAssignmentTestRunner) BeginManagerAssignmentTx(context.Context) (managerAssignmentTx, error) {
	r.queries.beginCalls++
	r.queries.callOrder = append(r.queries.callOrder, "begin")
	return &managerAssignmentTestTx{queries: r.queries}, nil
}

func newManagerAssignmentTestServer() (*Server, *fakeManagerAssignmentQueries, *managerAuditCapture) {
	queries := newFakeManagerAssignmentQueries()
	auditCapture := &managerAuditCapture{}
	router := chi.NewRouter()
	s := &Server{
		router:                            router,
		managerAssignmentQueriesOverride:  queries,
		managerAssignmentTxRunnerOverride: managerAssignmentTestRunner{queries: queries},
		Audit:                             auditCapture,
	}
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	appManager := contract.AuthRequirement{Kind: contract.AuthAppManager}
	registerOpHTTP(router, http.MethodGet, "/api/prohibitorum/managed-applications/manager-candidates", appManager, s.handleListAppManagerCandidatesHTTP)
	registerOpHTTP(router, http.MethodGet, "/api/prohibitorum/oidc-applications/{clientId}/managers", admin, s.handleListOIDCApplicationManagersHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/oidc-applications/{clientId}/managers", admin, s.handleAssignOIDCApplicationManagerHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/oidc-applications/{clientId}/managers/remove", admin, s.handleRemoveOIDCApplicationManagerHTTP)
	registerOpHTTP(router, http.MethodGet, "/api/prohibitorum/forward-auth-apps/{clientId}/managers", admin, s.handleListForwardAuthAppManagersHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/forward-auth-apps/{clientId}/managers", admin, s.handleAssignForwardAuthAppManagerHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/forward-auth-apps/{clientId}/managers/remove", admin, s.handleRemoveForwardAuthAppManagerHTTP)
	registerOpHTTP(router, http.MethodGet, "/api/prohibitorum/saml-applications/{id}/managers", admin, s.handleListSAMLApplicationManagersHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/saml-applications/{id}/managers", admin, s.handleAssignSAMLApplicationManagerHTTP)
	s.registerSudoOpHTTP(router, http.MethodPost, "/api/prohibitorum/saml-applications/{id}/managers/remove", admin, s.handleRemoveSAMLApplicationManagerHTTP)
	return s, queries, auditCapture
}

func TestListAppManagerCandidatesReturnsSafeSummariesToAppManagers(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	queries.candidates = []db.ListActiveAppManagerCandidatesRow{{ID: 7, Username: "grace", DisplayName: "Grace Hopper"}}
	sess := &authn.Session{Account: &db.Account{ID: 9, Role: "app_manager"}, Data: &authn.SessionData{}}
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, reqWithSession(http.MethodGet, "/api/prohibitorum/managed-applications/manager-candidates?q=%20Grace%20", "", "", sess))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	if queries.candidateQuery != "Grace" {
		t.Fatalf("candidate query = %q, want Grace", queries.candidateQuery)
	}
	var page contract.Page[contract.AccountSummaryView]
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 7 || page.Items[0].Username != "grace" || page.NextCursor != "" {
		t.Fatalf("candidate page = %#v", page)
	}
}

func TestListAppManagerCandidatesRejectsEmptySearch(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	recorder := runManagerRequest(t, s, http.MethodGet, "/api/prohibitorum/managed-applications/manager-candidates", "", time.Time{})
	assertManagerAPIError(t, recorder, http.StatusBadRequest, "bad_request")
	if queries.candidateQuery != "" {
		t.Fatalf("candidate query ran with %q", queries.candidateQuery)
	}
}

func assertManagerCallOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order = %#v, want %#v", got, want)
		}
	}
}

func runManagerRequest(t *testing.T, s *Server, method, path, body string, sudoUntil time.Time) *httptest.ResponseRecorder {
	t.Helper()
	session := adminSession(sudoUntil)
	session.Account.ID = 99
	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, reqWithSession(method, path, body, "", session))
	return recorder
}

func assertManagerAPIError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
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

func seedManagerAssignmentFixtures(q *fakeManagerAssignmentQueries) {
	q.oidcClients["wiki"] = db.OidcClient{ClientID: "wiki"}
	q.oidcClients["forward"] = db.OidcClient{ClientID: "forward", ForwardAuthEnabled: true}
	q.samlSPs[7] = db.SamlSp{ID: 7, EntityID: "urn:test:saml"}
	q.accounts[7] = db.Account{ID: 7, Username: "manager", DisplayName: "Manager", Role: "app_manager"}
}

func TestListOIDCManagerAssignments(t *testing.T) {
	s, queries, auditCapture := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)
	assignedAt := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	queries.oidcManagers["wiki"] = map[int32]time.Time{7: assignedAt}

	recorder := runManagerRequest(t, s, http.MethodGet, "/api/prohibitorum/oidc-applications/wiki/managers", "", time.Time{})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	var managers []contract.AppManagerView
	if err := json.Unmarshal(recorder.Body.Bytes(), &managers); err != nil {
		t.Fatalf("decode manager list: %v", err)
	}
	if len(managers) != 1 {
		t.Fatalf("managers = %#v, want one row", managers)
	}
	if got := managers[0]; got.ID != 7 || got.Username != "manager" || got.DisplayName != "Manager" || got.Disabled || !got.AssignedAt.Equal(assignedAt) {
		t.Fatalf("manager = %#v", got)
	}
	if len(auditCapture.records) != 0 {
		t.Fatalf("GET emitted audit records: %#v", auditCapture.records)
	}
}

func TestManagerAssignmentsRequireAdminAndFreshSudo(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)

	userSession := &authn.Session{Account: &db.Account{ID: 7, Role: "app_manager"}, Data: &authn.SessionData{SudoUntil: time.Now().Add(time.Hour)}}
	userRecorder := httptest.NewRecorder()
	s.router.ServeHTTP(userRecorder, reqWithSession(http.MethodGet, "/api/prohibitorum/oidc-applications/wiki/managers", "", "", userSession))
	assertManagerAPIError(t, userRecorder, http.StatusForbidden, "not_admin")

	assignRecorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers", `{"accountId":7}`, time.Time{})
	assertManagerAPIError(t, assignRecorder, http.StatusUnauthorized, "sudo_required")
	removeRecorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers/remove", `{"accountId":7}`, time.Time{})
	assertManagerAPIError(t, removeRecorder, http.StatusUnauthorized, "sudo_required")
	if queries.assignOIDCCalls != 0 || queries.removeOIDCCalls != 0 {
		t.Fatalf("mutation ran before sudo: assign=%d remove=%d", queries.assignOIDCCalls, queries.removeOIDCCalls)
	}
}

func TestAssignOIDCManagerRejectsNonManagerRole(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)
	queries.accounts[7] = db.Account{ID: 7, Role: "user"}

	recorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers", `{"accountId":7}`, time.Now().Add(time.Hour))
	assertManagerAPIError(t, recorder, http.StatusBadRequest, "invalid_manager_role")
	if queries.assignOIDCCalls != 0 {
		t.Fatalf("assign calls = %d, want 0", queries.assignOIDCCalls)
	}
}

func TestAssignOIDCManagerLocksTargetBeforeInsertAndCommits(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)

	recorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers", `{"accountId":7}`, time.Now().Add(time.Hour))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", recorder.Code, recorder.Body.String())
	}
	assertManagerCallOrder(t, queries.callOrder, []string{"begin", "lock", "assign", "commit"})
	if queries.lockAccountCalls != 1 || queries.commitCalls != 1 || queries.rollbackCalls != 0 {
		t.Fatalf("transaction calls: locks=%d commits=%d rollbacks=%d", queries.lockAccountCalls, queries.commitCalls, queries.rollbackCalls)
	}
}

func TestAssignOIDCManagerRollsBackWhenValidationOrInsertFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		account   db.Account
		assignErr error
		status    int
		code      string
		wantOrder []string
	}{
		{
			name:      "target role changed before assignment",
			account:   db.Account{ID: 7, Role: "user"},
			status:    http.StatusBadRequest,
			code:      "invalid_manager_role",
			wantOrder: []string{"begin", "lock", "rollback"},
		},
		{
			name:      "assignment write fails",
			account:   db.Account{ID: 7, Role: "app_manager"},
			assignErr: errors.New("insert failed"),
			status:    http.StatusInternalServerError,
			code:      "server_error",
			wantOrder: []string{"begin", "lock", "assign", "rollback"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, queries, auditCapture := newManagerAssignmentTestServer()
			seedManagerAssignmentFixtures(queries)
			queries.accounts[7] = test.account
			queries.assignOIDCErr = test.assignErr

			recorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers", `{"accountId":7}`, time.Now().Add(time.Hour))
			assertManagerAPIError(t, recorder, test.status, test.code)
			assertManagerCallOrder(t, queries.callOrder, test.wantOrder)
			if queries.commitCalls != 0 || queries.rollbackCalls != 1 {
				t.Fatalf("transaction calls: commits=%d rollbacks=%d", queries.commitCalls, queries.rollbackCalls)
			}
			if len(auditCapture.records) != 0 {
				t.Fatalf("failed assignment emitted audit records: %#v", auditCapture.records)
			}
		})
	}
}

func TestManagerAssignmentRejectsDisabledOrMissingTarget(t *testing.T) {
	for _, test := range []struct {
		name       string
		account    db.Account
		accountID  int32
		wantStatus int
		wantCode   string
		endpoint   string
	}{
		{name: "disabled app manager assign", account: db.Account{ID: 7, Role: "app_manager", Disabled: true}, accountID: 7, endpoint: "/api/prohibitorum/oidc-applications/wiki/managers", wantStatus: http.StatusBadRequest, wantCode: "invalid_manager_role"},
		{name: "missing account", accountID: 23, endpoint: "/api/prohibitorum/oidc-applications/wiki/managers", wantStatus: http.StatusNotFound, wantCode: "account_not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, queries, _ := newManagerAssignmentTestServer()
			seedManagerAssignmentFixtures(queries)
			if test.accountID == 7 {
				queries.accounts[7] = test.account
			}
			recorder := runManagerRequest(t, s, http.MethodPost, test.endpoint, `{"accountId":`+strconv.FormatInt(int64(test.accountID), 10)+`}`, time.Now().Add(time.Hour))
			assertManagerAPIError(t, recorder, test.wantStatus, test.wantCode)
		})
	}
}

func TestRemoveOIDCManagerAllowsDisabledOrDemotedTarget(t *testing.T) {
	for _, test := range []struct {
		name    string
		account db.Account
	}{
		{name: "disabled app manager", account: db.Account{ID: 7, Role: "app_manager", Disabled: true}},
		{name: "demoted account", account: db.Account{ID: 7, Role: "user"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, queries, _ := newManagerAssignmentTestServer()
			seedManagerAssignmentFixtures(queries)
			queries.accounts[7] = test.account
			queries.oidcManagers["wiki"] = map[int32]time.Time{7: time.Now()}

			recorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers/remove", `{"accountId":7}`, time.Now().Add(time.Hour))
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204; body: %s", recorder.Code, recorder.Body.String())
			}
			if queries.removeOIDCCalls != 1 || queries.lockAccountCalls != 0 {
				t.Fatalf("remove calls=%d target locks=%d, want 1 and 0", queries.removeOIDCCalls, queries.lockAccountCalls)
			}
		})
	}
}

func TestRemoveOIDCManagerRejectsInvalidAccountIDWithoutTargetLookup(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)

	recorder := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers/remove", `{"accountId":0}`, time.Now().Add(time.Hour))
	assertManagerAPIError(t, recorder, http.StatusBadRequest, "bad_request")
	if queries.removeOIDCCalls != 0 || queries.lockAccountCalls != 0 {
		t.Fatalf("remove calls=%d target locks=%d, want both 0", queries.removeOIDCCalls, queries.lockAccountCalls)
	}
}

func TestAssignOIDCManagerIsIdempotentAndAudited(t *testing.T) {
	s, queries, auditCapture := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)
	path := "/api/prohibitorum/oidc-applications/wiki/managers"

	for range 2 {
		recorder := runManagerRequest(t, s, http.MethodPost, path, `{"accountId":7}`, time.Now().Add(time.Hour))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204; body: %s", recorder.Code, recorder.Body.String())
		}
	}
	if got := len(queries.oidcManagers["wiki"]); got != 1 {
		t.Fatalf("OIDC assignment count = %d, want 1", got)
	}
	if len(auditCapture.records) != 2 {
		t.Fatalf("audit records = %#v, want one per accepted assign", auditCapture.records)
	}
	for _, record := range auditCapture.records {
		if record.AccountID == nil || *record.AccountID != 99 || record.Factor != audit.FactorAppManager || record.Event != audit.EventAppManagerAssigned {
			t.Fatalf("audit record = %#v", record)
		}
		wantDetail := map[string]any{"target_account_id": int32(7), "app_kind": "oidc", "app_id": "wiki"}
		if len(record.Detail) != len(wantDetail) || record.Detail["target_account_id"] != wantDetail["target_account_id"] || record.Detail["app_kind"] != wantDetail["app_kind"] || record.Detail["app_id"] != wantDetail["app_id"] {
			t.Fatalf("audit detail = %#v, want %#v", record.Detail, wantDetail)
		}
	}
}

func TestOIDCManagerRoutesValidateAppKindAndMissingRemoval(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)

	wrongKind := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/forward/managers", `{"accountId":7}`, time.Now().Add(time.Hour))
	assertManagerAPIError(t, wrongKind, http.StatusNotFound, "client_not_found")
	missingRemoval := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/oidc-applications/wiki/managers/remove", `{"accountId":7}`, time.Now().Add(time.Hour))
	assertManagerAPIError(t, missingRemoval, http.StatusNotFound, "client_not_found")
	if queries.assignOIDCCalls != 0 {
		t.Fatalf("wrong-kind assign calls = %d, want 0", queries.assignOIDCCalls)
	}
}

func TestForwardAuthManagerAssignmentsUseBackingOIDCClient(t *testing.T) {
	s, queries, _ := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)

	assign := runManagerRequest(t, s, http.MethodPost, "/api/prohibitorum/forward-auth-apps/forward/managers", `{"accountId":7}`, time.Now().Add(time.Hour))
	if assign.Code != http.StatusNoContent {
		t.Fatalf("assign status = %d, want 204; body: %s", assign.Code, assign.Body.String())
	}
	list := runManagerRequest(t, s, http.MethodGet, "/api/prohibitorum/forward-auth-apps/forward/managers", "", time.Time{})
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body: %s", list.Code, list.Body.String())
	}
	var managers []contract.AppManagerView
	if err := json.Unmarshal(list.Body.Bytes(), &managers); err != nil || len(managers) != 1 || managers[0].ID != 7 {
		t.Fatalf("forward-auth managers = %#v, err = %v", managers, err)
	}
	wrongKind := runManagerRequest(t, s, http.MethodGet, "/api/prohibitorum/oidc-applications/forward/managers", "", time.Time{})
	assertManagerAPIError(t, wrongKind, http.StatusNotFound, "client_not_found")
	wrongSurface := runManagerRequest(t, s, http.MethodGet, "/api/prohibitorum/forward-auth-apps/wiki/managers", "", time.Time{})
	assertManagerAPIError(t, wrongSurface, http.StatusNotFound, "client_not_found")
}

func TestSAMLManagerAssignmentLifecycle(t *testing.T) {
	s, queries, auditCapture := newManagerAssignmentTestServer()
	seedManagerAssignmentFixtures(queries)
	assignPath := "/api/prohibitorum/saml-applications/7/managers"

	assign := runManagerRequest(t, s, http.MethodPost, assignPath, `{"accountId":7}`, time.Now().Add(time.Hour))
	if assign.Code != http.StatusNoContent {
		t.Fatalf("assign status = %d, want 204; body: %s", assign.Code, assign.Body.String())
	}
	list := runManagerRequest(t, s, http.MethodGet, assignPath, "", time.Time{})
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body: %s", list.Code, list.Body.String())
	}
	remove := runManagerRequest(t, s, http.MethodPost, assignPath+"/remove", `{"accountId":7}`, time.Now().Add(time.Hour))
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d, want 204; body: %s", remove.Code, remove.Body.String())
	}
	missingRemoval := runManagerRequest(t, s, http.MethodPost, assignPath+"/remove", `{"accountId":7}`, time.Now().Add(time.Hour))
	assertManagerAPIError(t, missingRemoval, http.StatusNotFound, "credential_not_found")
	if len(auditCapture.records) != 2 || auditCapture.records[1].Event != audit.EventAppManagerRemoved {
		t.Fatalf("audit records = %#v", auditCapture.records)
	}
}
