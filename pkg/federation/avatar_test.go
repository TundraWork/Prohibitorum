package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	avatarpkg "prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

func TestAvatarFetch_RejectsNonHTTPS(t *testing.T) {
	// Production default (allowPrivate=false): http and other schemes are rejected.
	if err := validateAvatarURL("http://example.com/a.png", false); err == nil {
		t.Fatal("want error for http URL when allowPrivate=false")
	}
	if err := validateAvatarURL("ftp://example.com/a.png", false); err == nil {
		t.Fatal("want error for ftp URL when allowPrivate=false")
	}
	if err := validateAvatarURL("ftp://example.com/a.png", true); err == nil {
		t.Fatal("want error for ftp URL even when allowPrivate=true (only http/https are ever allowed)")
	}
	// allowPrivate=true (trusted-internal-IdP / loopback-OP tests) additionally
	// permits plaintext http so a loopback OP can serve the picture.
	if err := validateAvatarURL("http://127.0.0.1:8080/a.png", true); err != nil {
		t.Fatalf("http should be allowed when allowPrivate=true, got %v", err)
	}
	if err := validateAvatarURL("https://example.com/a.png", false); err != nil {
		t.Fatalf("https must always be allowed, got %v", err)
	}
}

func TestAvatarFetch_RejectsNonImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	if _, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client(), false); err == nil || !strings.Contains(err.Error(), "content-type") {
		t.Fatalf("want content-type rejection, got %v", err)
	}
}

func TestAvatarFetch_ReturnsImageBytes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer srv.Close()
	b, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client(), false)
	if err != nil || len(b) == 0 {
		t.Fatalf("want bytes, got len=%d err=%v", len(b), err)
	}
}

type avatarManagerQueries struct {
	account     db.Account
	sources     []db.ListAvatarSourcesByAccountRow
	upserts     []db.UpsertAvatarSourceParams
	activations []db.SetActiveAvatarParams
}

func (q *avatarManagerQueries) GetAccountByID(context.Context, int32) (db.Account, error) {
	return q.account, nil
}

func (q *avatarManagerQueries) ListAvatarSourcesByAccount(context.Context, int32) ([]db.ListAvatarSourcesByAccountRow, error) {
	return q.sources, nil
}

func (q *avatarManagerQueries) UpsertAvatarSource(_ context.Context, params db.UpsertAvatarSourceParams) error {
	q.upserts = append(q.upserts, params)
	return nil
}

func (q *avatarManagerQueries) SetActiveAvatar(_ context.Context, params db.SetActiveAvatarParams) error {
	q.activations = append(q.activations, params)
	return nil
}

func avatarManagerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAvatarManagerInheritanceSelectionPolicy(t *testing.T) {
	const accountID int32 = 7
	const source = "upstream:corp"
	pngBytes := avatarManagerPNG(t)
	_, expectedETag, err := avatarpkg.Process(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name           string
		active         pgtype.Text
		existingETag   string
		wantUpsert     bool
		wantActivation bool
	}{
		{name: "null selects upstream", wantUpsert: true, wantActivation: true},
		{name: "user remains selected", active: pgtype.Text{String: "user", Valid: true}, wantUpsert: true},
		{name: "none remains selected", active: pgtype.Text{String: "none", Valid: true}, wantUpsert: true},
		{name: "changed current upstream refreshes selection", active: pgtype.Text{String: source, Valid: true}, existingETag: "old-etag", wantUpsert: true, wantActivation: true},
		{name: "different upstream does not steal selection", active: pgtype.Text{String: "upstream:other", Valid: true}, wantUpsert: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queries := &avatarManagerQueries{account: db.Account{ID: accountID, AvatarSource: test.active}}
			if test.existingETag != "" {
				queries.sources = []db.ListAvatarSourcesByAccountRow{{
					Source: source,
					Etag:   pgtype.Text{String: test.existingETag, Valid: true},
				}}
			}
			store := kv.NewMemoryStore()
			t.Cleanup(func() { _ = store.Close() })
			manager := NewAvatarManager(queries, store)
			manager.fetch = func(context.Context, string, bool) ([]byte, error) {
				return pngBytes, nil
			}
			provider := Provider{ID: 11, Slug: "corp", Config: json.RawMessage(`{"allowPrivateNetwork":true}`)}

			manager.run(context.Background(), accountID, provider, "https://cdn.test/avatar.png")

			if got := len(queries.upserts); got != boolCount(test.wantUpsert) {
				t.Fatalf("upserts = %d, want %d", got, boolCount(test.wantUpsert))
			}
			if got := len(queries.activations); got != boolCount(test.wantActivation) {
				t.Fatalf("activations = %d, want %d", got, boolCount(test.wantActivation))
			}
			if test.wantUpsert {
				upsert := queries.upserts[0]
				if upsert.Source != source || upsert.IdpID == nil || *upsert.IdpID != provider.ID {
					t.Fatalf("upsert = %+v, want source %q and provider %d", upsert, source, provider.ID)
				}
				if !upsert.Etag.Valid || upsert.Etag.String != expectedETag {
					t.Fatalf("upsert etag = %+v, want %q", upsert.Etag, expectedETag)
				}
			}
			if test.wantActivation && queries.activations[0].Source != source {
				t.Fatalf("activation source = %q, want %q", queries.activations[0].Source, source)
			}
			if _, err := store.Get(context.Background(), AvatarFetchKey(accountID, provider.ID)); err == nil {
				t.Fatal("completed inheritance left dedupe key behind")
			}
		})
	}
}

func TestAvatarManagerSkipsUnchangedETagRefresh(t *testing.T) {
	const accountID int32 = 7
	const source = "upstream:corp"
	pngBytes := avatarManagerPNG(t)
	_, etag, err := avatarpkg.Process(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	queries := &avatarManagerQueries{
		account: db.Account{ID: accountID, AvatarSource: pgtype.Text{String: source, Valid: true}},
		sources: []db.ListAvatarSourcesByAccountRow{{
			Source: source,
			Etag:   pgtype.Text{String: etag, Valid: true},
		}},
	}
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	manager := NewAvatarManager(queries, store)
	manager.fetch = func(context.Context, string, bool) ([]byte, error) {
		return pngBytes, nil
	}

	manager.run(context.Background(), accountID, Provider{ID: 11, Slug: "corp", Config: json.RawMessage(`{}`)}, "https://cdn.test/avatar.png")

	if len(queries.upserts) != 0 {
		t.Fatalf("unchanged avatar caused %d upserts", len(queries.upserts))
	}
	if len(queries.activations) != 0 {
		t.Fatalf("unchanged avatar caused %d activations", len(queries.activations))
	}
}

func TestAvatarManagerDedupesConcurrentProviderRefresh(t *testing.T) {
	const accountID int32 = 7
	provider := Provider{ID: 11, Slug: "corp", Config: json.RawMessage(`{}`)}
	queries := &avatarManagerQueries{account: db.Account{ID: accountID}}
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	if ok, err := store.SetNX(context.Background(), AvatarFetchKey(accountID, provider.ID), "1", time.Minute); err != nil || !ok {
		t.Fatalf("seed dedupe key: ok=%v err=%v", ok, err)
	}
	manager := NewAvatarManager(queries, store)
	fetched := false
	manager.fetch = func(context.Context, string, bool) ([]byte, error) {
		fetched = true
		return avatarManagerPNG(t), nil
	}

	manager.run(context.Background(), accountID, provider, "https://cdn.test/avatar.png")

	if fetched {
		t.Fatal("deduped refresh fetched the avatar")
	}
	if len(queries.upserts) != 0 || len(queries.activations) != 0 {
		t.Fatalf("deduped refresh mutated avatar state: upserts=%d activations=%d", len(queries.upserts), len(queries.activations))
	}
	if _, err := store.Get(context.Background(), AvatarFetchKey(accountID, provider.ID)); err != nil {
		t.Fatalf("deduped refresh removed another worker's key: %v", err)
	}
}

func TestAvatarManagerUsesProviderPrivateNetworkPolicy(t *testing.T) {
	queries := &avatarManagerQueries{account: db.Account{ID: 7}}
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	manager := NewAvatarManager(queries, store)
	var got bool
	manager.fetch = func(_ context.Context, _ string, allowPrivate bool) ([]byte, error) {
		got = allowPrivate
		return nil, nil
	}

	manager.run(context.Background(), 7, Provider{
		ID: 11, Slug: "corp", Config: json.RawMessage(`{"allowPrivateNetwork":true}`),
	}, "https://cdn.test/avatar.png")

	if !got {
		t.Fatal("avatar fetch did not receive provider allowPrivateNetwork policy")
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
