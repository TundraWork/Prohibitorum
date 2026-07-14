package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "APIKEY" || r.URL.Query().Get("steamids") != "76561198000000000" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"response":{"players":[{"personaname":"Gaben","avatarfull":"https://cdn/avatar.jpg"}]}}`))
	}))
	defer srv.Close()
	defer SetEndpoints(loginEndpoint, srv.URL)()

	s, err := FetchSummary(context.Background(), http.DefaultClient, "APIKEY", "76561198000000000")
	if err != nil || s.PersonaName != "Gaben" || s.AvatarURL != "https://cdn/avatar.jpg" {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
}

func TestFetchSummaryEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"response":{"players":[]}}`))
	}))
	defer srv.Close()
	defer SetEndpoints(loginEndpoint, srv.URL)()

	if _, err := FetchSummary(context.Background(), http.DefaultClient, "APIKEY", "76561198000000000"); err == nil {
		t.Fatal("expected error on empty player list")
	}
}
