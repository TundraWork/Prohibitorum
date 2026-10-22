// Package server — handle_me_preferences_test.go
//
// GET/PATCH /me/preferences through the real Huma path, so the schema's
// strictness (unknown sections and fields, missing fields) is exercised along
// with the handler's value checks and the section merge.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/weberr"
)

func preferencesAPI(t *testing.T, q *fakeUpdateMeQ) (http.Handler, *db.Account) {
	t.Helper()
	router, api := buildTestAPI(t)
	s := newUpdateMeServer(q)
	sessionReq := contract.AuthRequirement{Kind: contract.AuthSession}
	registerOp(api, contract.OperationGetMyPreferences, s.handleGetMyPreferences, sessionReq)
	registerOp(api, contract.OperationUpdateMyPreferences, s.handleUpdateMyPreferences, sessionReq)
	acct := &db.Account{ID: 7, Username: "ada", Preferences: q.stored}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := &authn.Session{Account: acct}
		router.ServeHTTP(w, r.WithContext(authn.WithSession(r.Context(), sess)))
	})
	return weberr.RequestID(h), acct
}

func servePreferences(h http.Handler, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/me/preferences", strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestGetMyPreferences(t *testing.T) {
	for _, tc := range []struct{ name, stored, want string }{
		{"never set", `{}`, `{"appearance":{"scheme":null,"mode":null}}`},
		{"set", `{"appearance":{"scheme":"nord","mode":"dark"}}`, `{"appearance":{"scheme":"nord","mode":"dark"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := preferencesAPI(t, &fakeUpdateMeQ{stored: []byte(tc.stored)})
			rr := servePreferences(h, http.MethodGet, "")
			if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != tc.want {
				t.Fatalf("GET = %d %s, want 200 %s", rr.Code, rr.Body, tc.want)
			}
		})
	}
}

func TestUpdateMyPreferences(t *testing.T) {
	const later = `{"appearance":{"scheme":"sand","mode":null},"editor":{"tabs":2}}`
	cases := []struct {
		name, body string
		status     int
		// stored is the document after the request; "" means no write.
		stored string
	}{
		{"sets the appearance", `{"appearance":{"scheme":"sakura","mode":"light"}}`, 200,
			`{"appearance":{"scheme":"sakura","mode":"light"},"editor":{"tabs":2}}`},
		{"clears a field with null", `{"appearance":{"scheme":null,"mode":"system"}}`, 200,
			`{"appearance":{"scheme":null,"mode":"system"},"editor":{"tabs":2}}`},
		{"no sections keeps everything", `{}`, 200, later},
		{"unknown scheme", `{"appearance":{"scheme":"solarized","mode":null}}`, 400, ""},
		{"unknown mode", `{"appearance":{"scheme":null,"mode":"dim"}}`, 400, ""},
		{"missing field", `{"appearance":{"scheme":"nord"}}`, 422, ""},
		{"unknown field", `{"appearance":{"scheme":"nord","mode":null,"font":"serif"}}`, 422, ""},
		{"unknown section", `{"editor":{"tabs":2}}`, 422, ""},
		{"a null section is left out", `{"appearance":null}`, 200, later},
		{"wrong type", `{"appearance":{"scheme":1,"mode":null}}`, 422, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeUpdateMeQ{stored: []byte(later)}
			h, acct := preferencesAPI(t, q)
			rr := servePreferences(h, http.MethodPatch, tc.body)
			if rr.Code != tc.status {
				t.Fatalf("PATCH %s = %d %s, want %d", tc.body, rr.Code, rr.Body, tc.status)
			}
			if tc.status == http.StatusBadRequest && !strings.Contains(rr.Body.String(), `"code":"bad_request"`) {
				t.Fatalf("PATCH %s body = %s, want bad_request", tc.body, rr.Body)
			}
			if tc.stored == "" {
				if len(q.preferenceCalls) != 0 {
					t.Fatalf("rejected request wrote %s", q.preferenceCalls[0].Sections)
				}
				return
			}
			if !sameJSON(t, q.stored, tc.stored) {
				t.Fatalf("stored = %s, want %s", q.stored, tc.stored)
			}
			if !sameJSON(t, acct.Preferences, tc.stored) {
				t.Fatalf("session account preferences = %s, want the stored document", acct.Preferences)
			}
		})
	}
}

func sameJSON(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(g, w)
}

func TestSessionViewCarriesPreferences(t *testing.T) {
	s := &Server{}
	v := s.sessionView(&db.Account{Preferences: []byte(`{"appearance":{"scheme":"lagoon","mode":null}}`)})
	if v.Preferences.Appearance.Scheme == nil || *v.Preferences.Appearance.Scheme != "lagoon" || v.Preferences.Appearance.Mode != nil {
		t.Fatalf("sessionView preferences = %+v", v.Preferences.Appearance)
	}
}
