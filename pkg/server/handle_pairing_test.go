package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"prohibitorum/pkg/credential/pairing"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// newPairingTestServer builds a Server wired for the pairing-complete
// handler: it reuses newTestServer (which provides kvStore, sessionStore,
// fake queries, audit, rateLimiter, clientIP) and adds the pairingStore +
// accountLookup override the handler needs.
func newPairingTestServer(t *testing.T) (*Server, *fakeAuthQueries) {
	t.Helper()
	s, f, _ := newTestServer(t)
	s.pairingStore = pairing.NewPairingStore(s.kvStore)
	// accountLookupQ() override so the handler can call GetAccountByID
	// without a live *db.Queries. fakeAuthQueries returns a synthetic
	// enabled account for any ID, which is what the pairing-complete
	// happy path needs.
	s.accountLookup = f
	return s, f
}

// pairCompleteRequest builds a POST /pair/complete request body.
func pairCompleteRequest(t *testing.T, pairingID, returnTo string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"pairingId": pairingID})
	path := "/api/prohibitorum/auth/devices/pair/complete"
	if returnTo != "" {
		path += "?return_to=" + url.QueryEscape(returnTo)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	return req
}

// TestHandlePairComplete_ConcurrentExactlyOneSession proves the P1 race fix
// at the handler level: two concurrent /pair/complete calls on the SAME
// approved pairing produce exactly one 200 (session issued) and one
// pairing_not_found error. Before the fix, both could pass the status check
// and both issue sessions.
func TestHandlePairComplete_ConcurrentExactlyOneSession(t *testing.T) {
	s, f := newPairingTestServer(t)
	ctx := context.Background()
	const accountID = int32(42)
	f.accounts[accountID] = db.Account{ID: accountID, Username: "alice"}

	// Create + approve a pairing.
	p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loaded, err := s.pairingStore.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.pairingStore.Approve(ctx, loaded, accountID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	var okCount, errCount int64

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			s.handlePairCompleteHTTP(w, pairCompleteRequest(t, p.ID, ""))
			if w.Code == http.StatusOK {
				atomic.AddInt64(&okCount, 1)
			} else {
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if okCount != 1 {
		t.Fatalf("OK responses = %d, want exactly 1 (errs=%d)", okCount, errCount)
	}
	if errCount != n-1 {
		t.Fatalf("error responses = %d, want %d (oks=%d)", errCount, n-1, okCount)
	}

	// The canonical key now holds a consumed marker (not deleted) so a
	// second complete cannot resurrect the pairing. The code-index key
	// is gone (deleted by the winner).
	consumed, err := s.pairingStore.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after concurrent complete: %v (canonical key should hold consumed marker)", err)
	}
	if consumed.Status != pairing.PairingConsumed {
		t.Fatalf("canonical status after concurrent complete = %q, want %q", consumed.Status, pairing.PairingConsumed)
	}
	if _, err := s.kvStore.Get(ctx, "pairing:code:"+p.Code); err != kv.ErrKeyNotFound {
		t.Fatalf("code key after concurrent complete: err=%v, want ErrKeyNotFound", err)
	}
}

// TestHandlePairComplete_PendingFailsClosed ensures a pending (un-approved)
// pairing is rejected with pairing_not_approved (412) WITHOUT mutating the
// KV — the pairing stays pending and readable so the user can still approve
// it. A premature /complete must not destroy a valid pending ceremony.
func TestHandlePairComplete_PendingFailsClosed(t *testing.T) {
	s, _ := newPairingTestServer(t)
	ctx := context.Background()
	p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := httptest.NewRecorder()
	s.handlePairCompleteHTTP(w, pairCompleteRequest(t, p.ID, ""))
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("pending complete: status = %d, want %d (pairing_not_approved)", w.Code, http.StatusPreconditionRequired)
	}
	// The pending pairing must still be present and still pending.
	stillPending, err := s.pairingStore.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after pending complete: %v (pairing must be preserved)", err)
	}
	if stillPending.Status != pairing.PairingPending {
		t.Fatalf("status after pending complete = %q, want %q (must not mutate)", stillPending.Status, pairing.PairingPending)
	}
}

// TestHandlePairComplete_MissingFailsClosed ensures a nonexistent pairingID
// returns pairing_not_found (404) without issuing a session.
func TestHandlePairComplete_MissingFailsClosed(t *testing.T) {
	s, _ := newPairingTestServer(t)
	w := httptest.NewRecorder()
	s.handlePairCompleteHTTP(w, pairCompleteRequest(t, "does-not-exist", ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing complete: status = %d, want %d (pairing_not_found)", w.Code, http.StatusNotFound)
	}
}

// TestHandlePairComplete_HappyPath verifies the single-winner flow issues a
// session with the correct account and returns a SessionView.
func TestHandlePairComplete_HappyPath(t *testing.T) {
	s, f := newPairingTestServer(t)
	ctx := context.Background()
	const accountID = int32(7)
	f.accounts[accountID] = db.Account{ID: accountID, Username: "bob"}

	p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loaded, err := s.pairingStore.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.pairingStore.Approve(ctx, loaded, accountID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	w := httptest.NewRecorder()
	s.handlePairCompleteHTTP(w, pairCompleteRequest(t, p.ID, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("happy path: status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp pairCompleteResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Session.ID != accountID {
		t.Fatalf("session account ID = %d, want %d", resp.Session.ID, accountID)
	}
	if resp.Redirect != "/" {
		t.Fatalf("redirect = %q, want /", resp.Redirect)
	}
	// Pairing consumed — canonical key holds consumed marker.
	consumed, err := s.pairingStore.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after happy path: %v (canonical key should hold consumed marker)", err)
	}
	if consumed.Status != pairing.PairingConsumed {
		t.Fatalf("status after happy path = %q, want %q", consumed.Status, pairing.PairingConsumed)
	}
}

func TestHandlePairComplete_ReturnTo(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"relative", "/oauth/authorize?client_id=x&response_type=code", "/oauth/authorize?client_id=x&response_type=code"},
		{"same-origin", "https://auth.example.test/oauth/authorize?client_id=x", "/oauth/authorize?client_id=x"},
		{"external", "https://evil.example/x", "/"},
		{"protocol-relative", "//evil.example/x", "/"},
		{"backslash", "/\\evil.example/x", "/"},
		{"javascript", "javascript:alert(1)", "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newPairingTestServer(t)
			s.config.OIDC.Issuer = "https://auth.example.test"
			f.accounts[7] = db.Account{ID: 7, Username: "bob"}
			ctx := context.Background()
			p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := s.pairingStore.LookupByCode(ctx, p.Code)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.pairingStore.Approve(ctx, loaded, 7); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.handlePairCompleteHTTP(w, pairCompleteRequest(t, p.ID, tc.input))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var resp pairCompleteResp
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Redirect != tc.want {
				t.Fatalf("redirect=%q want=%q", resp.Redirect, tc.want)
			}
			if len(w.Result().Cookies()) == 0 {
				t.Fatal("missing new session cookie")
			}
		})
	}
}

// TestHandlePairStatus_ConsumedMapsToExpired proves the server-boundary
// mapping in handlePairStatusHTTP: a canonical consumed pairing record
// (the terminal marker left in KV after /pair/complete) is returned to the
// anonymous polling client as the existing terminal "expired" public
// state — never the internal "consumed" store state. The frontend Status
// union is pending|approved|expired; "consumed" is not a public state and
// would wedge the polling loop without a retry path.
func TestHandlePairStatus_ConsumedMapsToExpired(t *testing.T) {
	s, f := newPairingTestServer(t)
	ctx := context.Background()
	const accountID = int32(11)
	f.accounts[accountID] = db.Account{ID: accountID, Username: "carol"}

	// Create → approve → consume, leaving the consumed marker in KV.
	p, err := s.pairingStore.New(ctx, "ua/test", "127.0.0.1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	loaded, err := s.pairingStore.LookupByCode(ctx, p.Code)
	if err != nil {
		t.Fatalf("LookupByCode: %v", err)
	}
	if err := s.pairingStore.Approve(ctx, loaded, accountID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	w := httptest.NewRecorder()
	s.handlePairCompleteHTTP(w, pairCompleteRequest(t, p.ID, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// The canonical record is now the consumed marker.
	consumed, err := s.pairingStore.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID after consume: %v", err)
	}
	if consumed.Status != pairing.PairingConsumed {
		t.Fatalf("canonical status = %q, want %q (internal consumed marker)", consumed.Status, pairing.PairingConsumed)
	}

	// The anonymous status poll must surface "expired", not "consumed".
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/auth/devices/pair/status?id="+p.ID, nil)
	sw := httptest.NewRecorder()
	s.handlePairStatusHTTP(sw, req)
	if sw.Code != http.StatusOK {
		t.Fatalf("status poll: HTTP %d, want 200; body=%s", sw.Code, sw.Body.String())
	}
	var resp pairStatusResp
	if err := json.Unmarshal(sw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if resp.Status != "expired" {
		t.Fatalf("public status = %q, want %q (consumed must map to expired)", resp.Status, "expired")
	}
}
