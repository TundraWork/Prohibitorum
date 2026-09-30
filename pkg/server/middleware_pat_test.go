package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/diagnostic"
	sessstore "prohibitorum/pkg/session"
)

const testPATToken = pat.Prefix + "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"

// fakePATAuthQ serves one token and one owner.
type fakePATAuthQ struct {
	mu      sync.Mutex
	row     db.PersonalAccessToken
	missing bool
	acct    db.Account
	noAcct  bool
	touched []int32
	lookups int
	// lookupErr / acctErr stand in for a database failure (not a missing row).
	lookupErr error
	acctErr   error
}

func (f *fakePATAuthQ) GetPATByTokenHash(_ context.Context, hash []byte) (db.PersonalAccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.lookupErr != nil {
		return db.PersonalAccessToken{}, f.lookupErr
	}
	if f.missing || string(hash) != string(pat.HashToken(testPATToken)) {
		return db.PersonalAccessToken{}, pgx.ErrNoRows
	}
	return f.row, nil
}

func (f *fakePATAuthQ) GetAccountByID(_ context.Context, _ int32) (db.Account, error) {
	if f.acctErr != nil {
		return db.Account{}, f.acctErr
	}
	if f.noAcct {
		return db.Account{}, pgx.ErrNoRows
	}
	return f.acct, nil
}

func (f *fakePATAuthQ) TouchPATLastUsed(_ context.Context, id int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, id)
	return nil
}

type recordingAudit struct {
	mu   sync.Mutex
	recs []audit.Record
	ctxs []context.Context
}

func (r *recordingAudit) Record(ctx context.Context, rec audit.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec)
	r.ctxs = append(r.ctxs, ctx)
	return nil
}

// recordingDiag keeps the diagnostic records the capture middleware writes.
type recordingDiag struct {
	mu   sync.Mutex
	recs []diagnostic.Record
}

func (d *recordingDiag) Record(_ context.Context, rec diagnostic.Record) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recs = append(d.recs, rec)
	return nil
}

func newFakePATAuthQ(access pat.Access, role string) *fakePATAuthQ {
	return &fakePATAuthQ{
		row:  db.PersonalAccessToken{ID: 11, AccountID: 5, Access: string(access)},
		acct: db.Account{ID: 5, Username: "owner", Role: role},
	}
}

func codeOf(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rr.Body.String(), err)
	}
	return body.Code
}

// patTestRouter mounts patAuthMW in front of a stub API: a public route, a
// protected typed op, sudo-gated typed and raw ops, and a browser-only op.
type patTestRouter struct {
	*chi.Mux
	sawSession   *authn.Session // SessionFromContext in the public handler
	sawPrincipal *authn.Session // PrincipalFromContext in the public handler
	handled      []string
	fresh        func(*authn.Session) bool
	diag         *recordingDiag
}

func newPATTestRouter(q patAuthQueries, w audit.Writer) *patTestRouter {
	r := &patTestRouter{Mux: chi.NewMux(), diag: &recordingDiag{}}
	r.Use(func(next http.Handler) http.Handler { // request id, as in production
		return next
	})
	// A cookie session, when the test sets X-Test-Cookie, so fallback is visible.
	// Like LoadSession it runs first and leaves PAT requests alone.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Header.Get("X-Test-Cookie") != "" && !pat.SelectsManagementAuth(req) {
				req = req.WithContext(authn.WithSession(req.Context(), &authn.Session{
					Account: &db.Account{ID: 99, Role: "admin"}, Token: "t", Data: &authn.SessionData{SessionID: "sid"},
				}))
			}
			next.ServeHTTP(rw, req)
		})
	})
	r.Use(diagnosticCaptureMW(r.diag))
	r.Use(patAuthMW(q, w))
	api := humachi.New(r.Mux, humaConfig())
	registerSecurityScheme(api, sessstore.SessionCookieName)
	mgmt := huma.NewGroup(api, "/api/prohibitorum")
	s := &Server{}

	note := func(name string) { r.handled = append(r.handled, name) }
	type out struct {
		Body struct {
			AccountID int32  `json:"accountId"`
			ViaPAT    bool   `json:"viaPat"`
			Name      string `json:"name"`
		}
	}
	whoami := func(name string) func(context.Context, *struct{}) (*out, error) {
		return func(ctx context.Context, _ *struct{}) (*out, error) {
			note(name)
			sess := authn.SessionFromContext(ctx)
			o := &out{}
			o.Body.Name = name
			if sess != nil {
				o.Body.AccountID = sess.Account.ID
				o.Body.ViaPAT = sess.PAT != nil
			}
			return o, nil
		}
	}
	op := func(id, method, path string) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path}
	}
	sessionReq := contract.AuthRequirement{Kind: contract.AuthSession}
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	browser := contract.AuthRequirement{Kind: contract.AuthSession, BrowserOnly: true}

	registerOp(mgmt, op("pub", "GET", "/pub"), func(ctx context.Context, _ *struct{}) (*out, error) {
		note("pub")
		r.sawSession = authn.SessionFromContext(ctx)
		r.sawPrincipal = authn.PrincipalFromContext(ctx)
		return &out{}, nil
	}, contract.AuthRequirement{Kind: contract.AuthPublic})
	registerOp(mgmt, op("me", "GET", "/me"), whoami("me"), sessionReq)
	registerOp(mgmt, op("adm", "GET", "/adm"), whoami("adm"), admin)
	registerOp(mgmt, op("brw", "GET", "/brw"), whoami("brw"), browser)
	registerSudoOp(s, mgmt, op("sudo", "POST", "/sudo"), func(ctx context.Context, _ *struct{}) (*out, error) {
		return whoami("sudo")(ctx, nil)
	}, sessionReq)
	s.registerSudoOpHTTP(r.Mux, "POST", "/api/prohibitorum/sudo-raw", sessionReq, func(w http.ResponseWriter, req *http.Request) {
		note("sudo-raw")
		w.WriteHeader(http.StatusNoContent)
	})
	registerOpHTTP(r.Mux, "GET", "/api/prohibitorum/raw", sessionReq, func(w http.ResponseWriter, req *http.Request) {
		note("raw")
		sess := authn.SessionFromContext(req.Context())
		if sess == nil || sess.PAT == nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Inline sudo check, as handlers using requireFreshSudo do.
	registerOpHTTP(r.Mux, "POST", "/api/prohibitorum/sudo-inline", sessionReq, func(w http.ResponseWriter, req *http.Request) {
		if s.requireFreshSudo(req.Context(), w, authn.SessionFromContext(req.Context())) {
			return
		}
		note("sudo-inline")
		w.WriteHeader(http.StatusNoContent)
	})
	// Stand-in for the verify endpoint: sees the header, no PAT principal.
	registerOpHTTP(r.Mux, "GET", "/api/prohibitorum/forward-auth/verify", contract.AuthRequirement{Kind: contract.AuthPublic},
		func(w http.ResponseWriter, req *http.Request) {
			note("verify")
			if authn.HasPATPrincipal(req.Context()) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	return r
}

func (r *patTestRouter) do(method, path string, hdr map[string][]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	for k, v := range hdr {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func patHdr(vals ...string) map[string][]string {
	return map[string][]string{pat.HeaderName: vals}
}

func TestPATAuthMW_NoHeaderLeavesRequestToCookie(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "admin")
	r := newPATTestRouter(q, nil)
	rr := r.do("GET", "/api/prohibitorum/me", map[string][]string{"X-Test-Cookie": {"1"}})
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"accountId":99`) || strings.Contains(rr.Body.String(), `"viaPat":true`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if q.lookups != 0 {
		t.Errorf("token looked up without a header: %d", q.lookups)
	}
	if rr := r.do("GET", "/api/prohibitorum/me", nil); rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "no_session" {
		t.Errorf("no credentials: %d %s", rr.Code, rr.Body.String())
	}
}

func TestPATAuthMW_MalformedHeaderIs401AndNeverFallsBack(t *testing.T) {
	cases := map[string][]string{
		"empty":         {""},
		"two values":    {testPATToken, testPATToken},
		"wrong prefix":  {"Bearer " + testPATToken},
		"comma":         {testPATToken + "," + testPATToken},
		"inner space":   {testPATToken + " x"},
		"leading space": {" " + testPATToken},
		"trailing":      {testPATToken + " "},
		"tab":           {testPATToken + "\t"},
	}
	for name, vals := range cases {
		q := newFakePATAuthQ(pat.AccessSudo, "admin")
		r := newPATTestRouter(q, nil)
		hdr := patHdr(vals...)
		hdr["X-Test-Cookie"] = []string{"1"} // a valid cookie session must not rescue it
		rr := r.do("GET", "/api/prohibitorum/me", hdr)
		if rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "pat_invalid" {
			t.Errorf("%s: status=%d body=%s", name, rr.Code, rr.Body.String())
		}
		if q.lookups != 0 || len(r.handled) != 0 {
			t.Errorf("%s: lookups=%d handled=%v", name, q.lookups, r.handled)
		}
	}
}

func TestPATAuthMW_UnusableTokens(t *testing.T) {
	// Unknown, expired and revoked tokens all surface as "no row".
	q := newFakePATAuthQ(pat.AccessFull, "user")
	q.missing = true
	w := &recordingAudit{}
	r := newPATTestRouter(q, w)
	rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
	if rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "pat_invalid" {
		t.Fatalf("unknown: %d %s", rr.Code, rr.Body.String())
	}
	if len(w.recs) != 1 || w.recs[0].Factor != audit.FactorPAT || w.recs[0].Event != audit.EventFail || w.recs[0].Detail["reason"] != "pat_unknown" {
		t.Errorf("audit = %+v", w.recs)
	}

	q = newFakePATAuthQ(pat.AccessFull, "user")
	q.noAcct = true
	r = newPATTestRouter(q, nil)
	if rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken)); rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "pat_invalid" {
		t.Errorf("missing owner: %d %s", rr.Code, rr.Body.String())
	}

	q = newFakePATAuthQ(pat.AccessFull, "user")
	q.acct.Disabled = true
	r = newPATTestRouter(q, nil)
	if rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken)); rr.Code != http.StatusForbidden || codeOf(t, rr) != "account_disabled" {
		t.Errorf("disabled owner: %d %s", rr.Code, rr.Body.String())
	}
	if len(q.touched) != 0 {
		t.Errorf("disabled owner's token was touched")
	}
}

func TestPATAuthMW_AccessLevelsAndAPIAccess(t *testing.T) {
	for _, access := range []pat.Access{pat.AccessSelectedApps, pat.AccessAllApps} {
		q := newFakePATAuthQ(access, "admin")
		w := &recordingAudit{}
		r := newPATTestRouter(q, w)
		rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
		if rr.Code != http.StatusForbidden || codeOf(t, rr) != "pat_api_not_allowed" {
			t.Errorf("%s: %d %s", access, rr.Code, rr.Body.String())
		}
		if len(r.handled) != 0 || len(q.touched) != 0 {
			t.Errorf("%s: handler ran or token touched", access)
		}
		if len(w.recs) != 1 || w.recs[0].Detail["reason"] != "pat_api_not_allowed" {
			t.Errorf("%s: audit = %+v", access, w.recs)
		}
	}
	for _, access := range []pat.Access{pat.AccessFull, pat.AccessSudo} {
		q := newFakePATAuthQ(access, "user")
		r := newPATTestRouter(q, nil)
		rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"accountId":5`) || !strings.Contains(rr.Body.String(), `"viaPat":true`) {
			t.Errorf("%s: %d %s", access, rr.Code, rr.Body.String())
		}
		if len(q.touched) != 1 || q.touched[0] != 11 {
			t.Errorf("%s: last_used touches = %v", access, q.touched)
		}
	}
}

// The token acts as its owner: role checks still apply.
func TestPATAuthMW_OwnerRoleStillApplies(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessSudo, "user")
	r := newPATTestRouter(q, nil)
	if rr := r.do("GET", "/api/prohibitorum/adm", patHdr(testPATToken)); rr.Code != http.StatusForbidden || codeOf(t, rr) != "not_admin" {
		t.Errorf("non-admin owner on admin route: %d %s", rr.Code, rr.Body.String())
	}
	q = newFakePATAuthQ(pat.AccessFull, "admin")
	r = newPATTestRouter(q, nil)
	if rr := r.do("GET", "/api/prohibitorum/adm", patHdr(testPATToken)); rr.Code != http.StatusOK {
		t.Errorf("admin owner on admin route: %d %s", rr.Code, rr.Body.String())
	}
}

func TestPATAuthMW_AuthorizationHeaderIsNotAPAT(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessSudo, "admin")
	r := newPATTestRouter(q, nil)
	rr := r.do("GET", "/api/prohibitorum/me", map[string][]string{"Authorization": {"Bearer " + testPATToken}})
	if rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "no_session" || q.lookups != 0 {
		t.Errorf("status=%d body=%s lookups=%d", rr.Code, rr.Body.String(), q.lookups)
	}
}

func TestPATAuthMW_ForwardAuthVerifyIsLeftAlone(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessSelectedApps, "user")
	r := newPATTestRouter(q, nil)
	for _, vals := range [][]string{{testPATToken}, {""}, {" x "}} {
		rr := r.do("GET", "/api/prohibitorum/forward-auth/verify", patHdr(vals...))
		if rr.Code != http.StatusNoContent {
			t.Errorf("%q: status=%d body=%s", vals, rr.Code, rr.Body.String())
		}
	}
	if q.lookups != 0 {
		t.Errorf("middleware looked the token up for the gateway route: %d", q.lookups)
	}
}

// Public routes never see the PAT principal through SessionFromContext.
func TestPATAuthMW_PublicRoutesDoNotSeePATSession(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "admin")
	r := newPATTestRouter(q, nil)
	rr := r.do("GET", "/api/prohibitorum/pub", patHdr(testPATToken))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if r.sawSession != nil {
		t.Errorf("SessionFromContext = %+v on a public route, want nil", r.sawSession)
	}
	if r.sawPrincipal == nil || r.sawPrincipal.PAT == nil {
		t.Errorf("PrincipalFromContext = %+v, want the PAT session", r.sawPrincipal)
	}
}

func TestPATSession_ReachesRawAndTypedHandlers(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "user")
	r := newPATTestRouter(q, nil)
	if rr := r.do("GET", "/api/prohibitorum/raw", patHdr(testPATToken)); rr.Code != http.StatusNoContent {
		t.Errorf("raw handler: %d", rr.Code)
	}
	if rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken)); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"viaPat":true`) {
		t.Errorf("typed handler: %d %s", rr.Code, rr.Body.String())
	}
}

// A database failure is a server error, not a verdict on the token.
func TestPATAuthMW_LookupFailureIsServerError(t *testing.T) {
	for name, set := range map[string]func(*fakePATAuthQ){
		"token lookup": func(q *fakePATAuthQ) { q.lookupErr = context.DeadlineExceeded },
		"owner lookup": func(q *fakePATAuthQ) { q.acctErr = context.DeadlineExceeded },
	} {
		q := newFakePATAuthQ(pat.AccessFull, "user")
		set(q)
		w := &recordingAudit{}
		r := newPATTestRouter(q, w)
		rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
		if rr.Code != http.StatusInternalServerError || codeOf(t, rr) != "server_error" {
			t.Errorf("%s: %d %s", name, rr.Code, rr.Body.String())
		}
		if len(w.recs) != 0 {
			t.Errorf("%s: audited %+v", name, w.recs)
		}
	}
}

// Missing and disabled owners are audited on the API as at the gateway.
func TestPATAuthMW_OwnerRefusalsAreAudited(t *testing.T) {
	for reason, set := range map[string]func(*fakePATAuthQ){
		"account_missing":  func(q *fakePATAuthQ) { q.noAcct = true },
		"account_disabled": func(q *fakePATAuthQ) { q.acct.Disabled = true },
	} {
		q := newFakePATAuthQ(pat.AccessFull, "user")
		set(q)
		w := &recordingAudit{}
		r := newPATTestRouter(q, w)
		r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
		if len(w.recs) != 1 || w.recs[0].Detail["reason"] != reason || w.recs[0].AccountID == nil || *w.recs[0].AccountID != 5 {
			t.Errorf("%s: audit = %+v", reason, w.recs)
		}
	}
}

// last_used_at counts requests the route served, not ones it refused.
func TestPATAuthMW_LastUsedOnlyOnServedRequests(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "user")
	r := newPATTestRouter(q, nil)
	for _, path := range []string{"/api/prohibitorum/adm", "/api/prohibitorum/brw"} {
		if rr := r.do("GET", path, patHdr(testPATToken)); rr.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
		}
	}
	if rr := r.do("POST", "/api/prohibitorum/sudo", patHdr(testPATToken)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("sudo: %d %s", rr.Code, rr.Body.String())
	}
	if len(q.touched) != 0 {
		t.Errorf("refused requests touched the token: %v", q.touched)
	}
	if rr := r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken)); rr.Code != http.StatusOK {
		t.Fatalf("me: %d", rr.Code)
	}
	if len(q.touched) != 1 {
		t.Errorf("served request touches = %v", q.touched)
	}
}

// PAT refusals reach the diagnostic capture, with the owner once it is known.
func TestPATAuthMW_RefusalsAreCapturedForDiagnostics(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "user")
	r := newPATTestRouter(q, nil)
	r.do("GET", "/api/prohibitorum/me", patHdr("not-a-token"))
	q.row.Access = string(pat.AccessAllApps)
	r.do("GET", "/api/prohibitorum/me", patHdr(testPATToken))
	q.row.Access = string(pat.AccessFull)
	r.do("GET", "/api/prohibitorum/adm", patHdr(testPATToken))
	if len(r.diag.recs) != 3 {
		t.Fatalf("diagnostic records = %+v", r.diag.recs)
	}
	for i, want := range []struct {
		code    string
		account bool
	}{{"pat_invalid", false}, {"pat_api_not_allowed", true}, {"not_admin", true}} {
		rec := r.diag.recs[i]
		if rec.Code != want.code || (rec.AccountID != nil) != want.account || (want.account && *rec.AccountID != 5) {
			t.Errorf("record %d = code %q account %v, want %q account=%v", i, rec.Code, rec.AccountID, want.code, want.account)
		}
	}
}

func TestPATSession_BrowserOnlyRoutesRejectPAT(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessSudo, "admin")
	r := newPATTestRouter(q, nil)
	rr := r.do("GET", "/api/prohibitorum/brw", patHdr(testPATToken))
	if rr.Code != http.StatusForbidden || codeOf(t, rr) != "pat_browser_session_required" {
		t.Errorf("%d %s", rr.Code, rr.Body.String())
	}
	if len(r.handled) != 0 {
		t.Errorf("handler ran: %v", r.handled)
	}
	if rr := r.do("GET", "/api/prohibitorum/brw", map[string][]string{"X-Test-Cookie": {"1"}}); rr.Code != http.StatusOK {
		t.Errorf("cookie session on browser-only route: %d %s", rr.Code, rr.Body.String())
	}
}

// full never passes a sudo check; sudo always does. Covers the typed op, the
// raw wrapper and an in-handler requireFreshSudo.
func TestPATSession_SudoLevels(t *testing.T) {
	routes := []struct{ method, path, name string }{
		{"POST", "/api/prohibitorum/sudo", "sudo"},
		{"POST", "/api/prohibitorum/sudo-raw", "sudo-raw"},
		{"POST", "/api/prohibitorum/sudo-inline", "sudo-inline"},
	}
	for _, rt := range routes {
		q := newFakePATAuthQ(pat.AccessFull, "user")
		r := newPATTestRouter(q, nil)
		hdr := patHdr(testPATToken)
		hdr["Content-Type"] = []string{"application/json"}
		rr := r.do(rt.method, rt.path, hdr)
		if rr.Code != http.StatusUnauthorized || codeOf(t, rr) != "sudo_required" || len(r.handled) != 0 {
			t.Errorf("full on %s: %d %s handled=%v", rt.name, rr.Code, rr.Body.String(), r.handled)
		}

		q = newFakePATAuthQ(pat.AccessSudo, "user")
		r = newPATTestRouter(q, nil)
		rr = r.do(rt.method, rt.path, hdr)
		if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent {
			t.Errorf("sudo on %s: %d %s", rt.name, rr.Code, rr.Body.String())
		}
		if len(r.handled) != 1 || r.handled[0] != rt.name {
			t.Errorf("sudo on %s: handled=%v", rt.name, r.handled)
		}
	}
}

func TestHasFreshSudo_PATIgnoresSessionWindow(t *testing.T) {
	s := &Server{}
	if s.hasFreshSudo(&authn.Session{PAT: &authn.PATPrincipal{Access: pat.AccessFull}}) {
		t.Error("full must not have fresh sudo")
	}
	if !s.hasFreshSudo(&authn.Session{PAT: &authn.PATPrincipal{Access: pat.AccessSudo}}) {
		t.Error("sudo must have fresh sudo")
	}
	// A PAT session never carries cookie data, so even a stale grant on Data is
	// not consulted.
	if s.hasFreshSudo(&authn.Session{
		PAT:  &authn.PATPrincipal{Access: pat.AccessFull},
		Data: &authn.SessionData{SudoUntil: time.Now().Add(time.Hour)},
	}) {
		t.Error("full must not inherit a session sudo window")
	}
}

// Events recorded while a PAT acts carry pat_id, via the request context.
func TestPATAuthMW_AuditContextCarriesPATID(t *testing.T) {
	q := newFakePATAuthQ(pat.AccessFull, "user")
	r := newPATTestRouter(q, nil)
	var captured context.Context
	r.Get("/api/prohibitorum/ctx", func(w http.ResponseWriter, req *http.Request) {
		captured = req.Context()
	})
	r.do("GET", "/api/prohibitorum/ctx", patHdr(testPATToken))
	if captured == nil {
		t.Fatal("handler not reached")
	}
	cap := &captureAudit{}
	if err := audit.NewWriter(cap).Record(captured, audit.Record{Factor: audit.FactorPassword, Event: audit.EventUse}); err != nil {
		t.Fatal(err)
	}
	var detail map[string]any
	if err := json.Unmarshal(cap.detail, &detail); err != nil || detail["pat_id"] != float64(11) {
		t.Errorf("detail = %s (%v)", cap.detail, err)
	}
}

type captureAudit struct {
	db.Querier
	detail []byte
}

func (c *captureAudit) InsertCredentialEvent(_ context.Context, p db.InsertCredentialEventParams) error {
	c.detail = p.Detail
	return nil
}

// browserOnlyRoutes lists every route that must reject PAT callers. Each entry
// is served by the real router with a sudo-level PAT principal, so anything not
// marked BrowserOnly would reach a handler with nil dependencies.
var browserOnlyRoutes = []struct{ method, path string }{
	{"GET", "/api/prohibitorum/me/sudo/methods"},
	{"POST", "/api/prohibitorum/me/sudo/begin"},
	{"POST", "/api/prohibitorum/me/sudo/complete"},
	{"POST", "/api/prohibitorum/me/credentials/register/begin"},
	{"POST", "/api/prohibitorum/me/credentials/register/complete"},
	{"GET", "/api/prohibitorum/consent"},
	{"POST", "/api/prohibitorum/consent"},
	{"GET", "/api/prohibitorum/saml-consent"},
	{"POST", "/api/prohibitorum/saml-consent"},
	{"GET", "/api/prohibitorum/me/identities/link/github/begin"},
	{"GET", "/api/prohibitorum/me/identities/link/github/callback"},
	{"POST", "/api/prohibitorum/identity-providers/github/tests"},
	{"GET", "/api/prohibitorum/identity-providers/github/tests/1"},
	{"POST", "/api/prohibitorum/identity-providers/github/tests/1/complete"},
	{"POST", "/api/prohibitorum/identity-providers/github/operator-session/start"},
	{"POST", "/api/prohibitorum/identity-providers/github/operator-session/verify"},
	{"POST", "/api/prohibitorum/identity-providers/github/operator-session/validate"},
	{"GET", "/api/prohibitorum/me/devices/pair/lookup"},
	{"POST", "/api/prohibitorum/me/devices/pair/approve"},
	{"POST", "/api/prohibitorum/me/devices/pair/cancel"},
}

func TestBrowserOnlyRoutesRejectPATCallers(t *testing.T) {
	router, _ := realAdminOnlyRouter(t)
	registered := map[string]bool{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	})
	principal := &authn.Session{
		Account: &db.Account{ID: 5, Role: "admin"},
		PAT:     &authn.PATPrincipal{ID: 11, Access: pat.AccessSudo},
	}
	for _, rt := range browserOnlyRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(""))
			req = req.WithContext(authn.WithPATSession(req.Context(), principal))
			rr := httptest.NewRecorder()
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						t.Errorf("handler ran and panicked on nil deps: %v — route is not BrowserOnly", rec)
					}
				}()
				router.ServeHTTP(rr, req)
			}()
			if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "pat_browser_session_required") {
				t.Errorf("status=%d body=%s; want 403 pat_browser_session_required", rr.Code, rr.Body.String())
			}
		})
	}
	// Catch a table entry that no longer matches a registered pattern (a typo
	// would otherwise test a 404 that happens to be neither).
	patterns := map[string]bool{}
	for k := range registered {
		patterns[k] = true
	}
	for _, rt := range browserOnlyRoutes {
		found := false
		for k := range patterns {
			parts := strings.SplitN(k, " ", 2)
			if parts[0] == rt.method && routeMatches(parts[1], rt.path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s %s matches no registered route", rt.method, rt.path)
		}
	}
}

// routeMatches compares a chi pattern with a concrete path, treating {param}
// segments as wildcards.
func routeMatches(pattern, path string) bool {
	ps, cs := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(ps) != len(cs) {
		return false
	}
	for i := range ps {
		if strings.HasPrefix(ps[i], "{") && strings.HasSuffix(ps[i], "}") {
			continue
		}
		if ps[i] != cs[i] {
			return false
		}
	}
	return true
}

// Non-admin PATs are held back during maintenance like cookie sessions.
func TestMaintenanceGate_AppliesToPATPrincipal(t *testing.T) {
	on := maintenanceGateMW(maintenanceResolver(true))
	q := newFakePATAuthQ(pat.AccessFull, "user")
	r := chi.NewMux()
	r.Use(patAuthMW(q, nil))
	r.Use(on)
	r.Get("/api/prohibitorum/me/tokens", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest("GET", "/api/prohibitorum/me/tokens", nil)
	req.Header.Set(pat.HeaderName, testPATToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable || codeOf(t, rr) != "maintenance_mode" {
		t.Fatalf("non-admin PAT in maintenance: %d %s", rr.Code, rr.Body.String())
	}

	q = newFakePATAuthQ(pat.AccessFull, "admin")
	r = chi.NewMux()
	r.Use(patAuthMW(q, nil))
	r.Use(on)
	r.Get("/api/prohibitorum/me/tokens", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("admin PAT in maintenance: %d %s", rr.Code, rr.Body.String())
	}
}
