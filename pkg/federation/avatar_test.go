package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	avatarpkg "prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
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

func TestAvatarFetchSanitizesURLFailureErrors(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		fetch   func(string) error
		want    string
		secrets []string
	}{
		{
			name:   "validation",
			rawURL: "https://userinfo-sentinel:password-sentinel@raw-url-sentinel.example/%zz?query=query-sentinel#fragment-sentinel",
			fetch: func(rawURL string) error {
				return validateAvatarURL(rawURL, false)
			},
			want: "avatar fetch invalid url",
			secrets: []string{
				"userinfo-sentinel", "password-sentinel", "raw-url-sentinel",
				"query-sentinel", "fragment-sentinel", "%zz",
			},
		},
		{
			name:   "request construction",
			rawURL: "https://userinfo-sentinel:password-sentinel@raw-url-sentinel.example/avatar.png?query=query-sentinel#fragment-sentinel",
			fetch: func(rawURL string) error {
				_, err := fetchUpstreamAvatarWithClient(nil, rawURL, http.DefaultClient, false)
				return err
			},
			want: "avatar fetch request error",
			secrets: []string{
				"userinfo-sentinel", "password-sentinel", "raw-url-sentinel",
				"query-sentinel", "fragment-sentinel", "nil Context",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fetch(tt.rawURL)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want safe category %q", err, tt.want)
			}
			if errors.Unwrap(err) != nil {
				t.Fatalf("safe avatar error exposes nested cause: %v", errors.Unwrap(err))
			}
			for _, secret := range tt.secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("avatar error leaked %q: %q", secret, err)
				}
			}
		})
	}
}

type failingAvatarTransport struct{}

func (failingAvatarTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial failed with Authorization: Bearer bearer-sentinel")
}

func TestAvatarManagerSanitizesNetworkFailureLogs(t *testing.T) {
	const rawURL = "https://avatar-user:avatar-password@cdn.example/avatar.png?signed=query-sentinel#fragment-sentinel"
	manager := NewAvatarManager(&avatarManagerQueries{})
	var logs bytes.Buffer
	manager.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	client := &http.Client{Transport: failingAvatarTransport{}}
	var fetchErr error
	manager.fetch = func(ctx context.Context, avatarURL string, allowPrivate bool) ([]byte, error) {
		_, fetchErr = fetchUpstreamAvatarWithClient(ctx, avatarURL, client, allowPrivate)
		return nil, fetchErr
	}

	manager.Refresh(context.Background(), 7, Provider{
		ID: 11, Slug: "corp", Config: json.RawMessage(`{}`),
	}, AvatarDelivery{URL: rawURL}, nil)
	if fetchErr == nil {
		t.Fatal("avatar fetch unexpectedly succeeded")
	}
	if errors.Unwrap(fetchErr) != nil {
		t.Fatalf("safe avatar error exposes nested transport cause: %v", errors.Unwrap(fetchErr))
	}
	if strings.Contains(fetchErr.Error(), "bearer-sentinel") {
		t.Fatalf("avatar error leaked nested transport text: %q", fetchErr)
	}

	logged := logs.String()
	for _, want := range []string{
		"upstream avatar fetch failed", "avatar fetch transport error", `"account_id":7`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log = %q, want safe context %q", logged, want)
		}
	}
	for _, secret := range []string{
		"avatar-user", "avatar-password", "query-sentinel", "fragment-sentinel", "bearer-sentinel",
	} {
		if strings.Contains(logged, secret) {
			t.Fatalf("network failure log leaked %q: %q", secret, logged)
		}
	}
}

func TestAvatarManagerSanitizesMalformedURLLogs(t *testing.T) {
	const rawURL = "https://userinfo-sentinel:password-sentinel@raw-url-sentinel.example/%zz?query=query-sentinel&transport=nested-transport-sentinel#fragment-sentinel"
	manager := NewAvatarManager(&avatarManagerQueries{})
	var logs bytes.Buffer
	manager.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	manager.Refresh(context.Background(), 7, Provider{
		ID: 11, Slug: "corp", Config: json.RawMessage(`{}`),
	}, AvatarDelivery{URL: rawURL}, nil)

	logged := logs.String()
	for _, want := range []string{
		"upstream avatar fetch failed", "avatar fetch invalid url", `"account_id":7`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log = %q, want safe context %q", logged, want)
		}
	}
	for _, secret := range []string{
		"userinfo-sentinel", "password-sentinel", "raw-url-sentinel", "%zz",
		"query-sentinel", "fragment-sentinel", "nested-transport-sentinel",
	} {
		if strings.Contains(logged, secret) {
			t.Fatalf("malformed URL log leaked %q: %q", secret, logged)
		}
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
	lists       int
	upserts     []db.UpsertAvatarSourceParams
	activations []db.SetActiveAvatarParams
	getErr      error
	listErr     error
	upsertErr   error
	activateErr error
}

func (q *avatarManagerQueries) GetAccountByID(context.Context, int32) (db.Account, error) {
	return q.account, q.getErr
}

func (q *avatarManagerQueries) ListAvatarSourcesByAccount(context.Context, int32) ([]db.ListAvatarSourcesByAccountRow, error) {
	q.lists++
	return q.sources, q.listErr
}

func (q *avatarManagerQueries) UpsertAvatarSource(_ context.Context, params db.UpsertAvatarSourceParams) error {
	q.upserts = append(q.upserts, params)
	return q.upsertErr
}

func (q *avatarManagerQueries) SetActiveAvatar(_ context.Context, params db.SetActiveAvatarParams) error {
	q.activations = append(q.activations, params)
	return q.activateErr
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

type avatarFallbackResolver struct {
	calls    int
	delivery AvatarDelivery
}

func (r *avatarFallbackResolver) ResolveAvatar(_ context.Context, _ Provider, delivery AvatarDelivery) (string, error) {
	r.calls++
	r.delivery = delivery
	return "https://cdn.test/fallback.png", nil
}

func TestAvatarManagerResolvesFallbackDuringRefresh(t *testing.T) {
	queries := &avatarManagerQueries{account: db.Account{ID: 7}}
	manager := NewAvatarManager(queries)
	resolver := &avatarFallbackResolver{}
	var fetchedURL string
	manager.fetch = func(_ context.Context, rawURL string, _ bool) ([]byte, error) {
		fetchedURL = rawURL
		return avatarManagerPNG(t), nil
	}
	opaque := &struct{ accessToken string }{accessToken: "opaque"}
	delivery := AvatarDelivery{Opaque: opaque}

	manager.Refresh(context.Background(), 7, Provider{
		ID: 11, Slug: "corp", Config: json.RawMessage(`{}`),
	}, delivery, resolver)

	if resolver.calls != 1 || fetchedURL != "https://cdn.test/fallback.png" ||
		resolver.delivery.Opaque != opaque {
		t.Fatalf("resolver calls=%d fetched=%q delivery=%v", resolver.calls, fetchedURL, resolver.delivery.Opaque)
	}
	if len(queries.upserts) != 1 || queries.upserts[0].UpstreamUrl.String != "https://cdn.test/fallback.png" {
		t.Fatalf("fallback delivery upserts = %+v, want one with the resolved URL", queries.upserts)
	}
}

func TestAvatarManagerRefreshSelectionPolicy(t *testing.T) {
	const accountID int32 = 7
	const source = "upstream:corp"
	pngBytes := avatarManagerPNG(t)
	_, expectedETag, err := avatarpkg.Process(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	selected := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	tests := []struct {
		name           string
		active         pgtype.Text
		selectedAt     pgtype.Timestamptz
		existingETag   string
		wantActivation bool
	}{
		{name: "null selects upstream", wantActivation: true},
		{name: "none selects upstream before any choice", active: pgtype.Text{String: "none", Valid: true}, wantActivation: true},
		{name: "chosen none remains selected", active: pgtype.Text{String: "none", Valid: true}, selectedAt: selected},
		{name: "user remains selected", active: pgtype.Text{String: "user", Valid: true}},
		{name: "changed current upstream refreshes selection", active: pgtype.Text{String: source, Valid: true}, existingETag: "old-etag", wantActivation: true},
		{name: "changed chosen upstream refreshes selection", active: pgtype.Text{String: source, Valid: true}, selectedAt: selected, existingETag: "old-etag", wantActivation: true},
		{name: "different upstream does not steal selection", active: pgtype.Text{String: "upstream:other", Valid: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queries := &avatarManagerQueries{account: db.Account{ID: accountID, AvatarSource: test.active, AvatarSelectedAt: test.selectedAt}}
			if test.existingETag != "" {
				queries.sources = []db.ListAvatarSourcesByAccountRow{{
					Source:      source,
					Etag:        pgtype.Text{String: test.existingETag, Valid: true},
					UpstreamUrl: pgtype.Text{String: "https://cdn.test/old.png", Valid: true},
				}}
			}
			manager := NewAvatarManager(queries)
			manager.fetch = func(context.Context, string, bool) ([]byte, error) {
				return pngBytes, nil
			}
			provider := Provider{ID: 11, Slug: "corp"}

			manager.Refresh(context.Background(), accountID, provider, AvatarDelivery{URL: "https://cdn.test/avatar.png"}, nil)

			if got := len(queries.upserts); got != 1 {
				t.Fatalf("upserts = %d, want 1", got)
			}
			upsert := queries.upserts[0]
			if upsert.Source != source || upsert.IdpID == nil || *upsert.IdpID != provider.ID {
				t.Fatalf("upsert = %+v, want source %q and provider %d", upsert, source, provider.ID)
			}
			if !upsert.Etag.Valid || upsert.Etag.String != expectedETag {
				t.Fatalf("upsert etag = %+v, want %q", upsert.Etag, expectedETag)
			}
			if got := len(queries.activations); got != boolCount(test.wantActivation) {
				t.Fatalf("activations = %d, want %d", got, boolCount(test.wantActivation))
			}
			if test.wantActivation && queries.activations[0].Source != source {
				t.Fatalf("activation source = %q, want %q", queries.activations[0].Source, source)
			}
		})
	}
}

func TestShouldActivate(t *testing.T) {
	const source = "upstream:corp"
	text := func(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }
	selected := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	tests := []struct {
		name       string
		active     pgtype.Text
		selectedAt pgtype.Timestamptz
		changed    bool
		want       bool
	}{
		{name: "never set, never chosen, new picture", changed: true, want: true},
		{name: "never set, never chosen, stored picture", want: true},
		{name: "none, never chosen", active: text("none"), want: true},
		{name: "none, chosen", active: text("none"), selectedAt: selected, changed: true},
		{name: "never set, chosen", selectedAt: selected, changed: true},
		{name: "upload active, never chosen", active: text("user"), changed: true},
		{name: "upload active, chosen", active: text("user"), selectedAt: selected, changed: true},
		{name: "other upstream active", active: text("upstream:other"), changed: true},
		{name: "this upstream active, picture changed", active: text(source), changed: true, want: true},
		{name: "this upstream chosen, picture changed", active: text(source), selectedAt: selected, changed: true, want: true},
		{name: "this upstream active, picture unchanged", active: text(source)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			account := db.Account{AvatarSource: test.active, AvatarSelectedAt: test.selectedAt}
			if got := shouldActivate(account, source, test.changed); got != test.want {
				t.Fatalf("shouldActivate = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAvatarManagerRecordsURLWhenETagUnchanged(t *testing.T) {
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
	manager := NewAvatarManager(queries)
	manager.fetch = func(context.Context, string, bool) ([]byte, error) {
		return pngBytes, nil
	}

	manager.Refresh(context.Background(), accountID, Provider{ID: 11, Slug: "corp", Config: json.RawMessage(`{}`)}, AvatarDelivery{URL: "https://cdn.test/avatar.png"}, nil)

	if len(queries.upserts) != 1 || queries.upserts[0].UpstreamUrl.String != "https://cdn.test/avatar.png" {
		t.Fatalf("upserts = %+v, want one recording the URL", queries.upserts)
	}
	if len(queries.activations) != 0 {
		t.Fatalf("unchanged avatar caused %d activations", len(queries.activations))
	}
}

func TestAvatarManagerSkipsFetchForStoredURL(t *testing.T) {
	const source = "upstream:corp"
	tests := []struct {
		name           string
		account        db.Account
		wantActivation bool
	}{
		{name: "active source unchanged", account: db.Account{ID: 7, AvatarSource: pgtype.Text{String: source, Valid: true}}},
		{name: "stored picture fills empty slot", account: db.Account{ID: 7}, wantActivation: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queries := &avatarManagerQueries{
				account: test.account,
				sources: []db.ListAvatarSourcesByAccountRow{{
					Source:      source,
					Etag:        pgtype.Text{String: "etag", Valid: true},
					UpstreamUrl: pgtype.Text{String: "https://cdn.test/avatar.png", Valid: true},
				}},
			}
			manager := NewAvatarManager(queries)
			fetched := false
			manager.fetch = func(context.Context, string, bool) ([]byte, error) {
				fetched = true
				return avatarManagerPNG(t), nil
			}

			manager.Refresh(context.Background(), 7, Provider{ID: 11, Slug: "corp"}, AvatarDelivery{URL: "https://cdn.test/avatar.png"}, nil)

			if fetched {
				t.Fatal("stored URL was fetched again")
			}
			if len(queries.upserts) != 0 {
				t.Fatalf("stored URL caused %d upserts", len(queries.upserts))
			}
			if got := len(queries.activations); got != boolCount(test.wantActivation) {
				t.Fatalf("activations = %d, want %d", got, boolCount(test.wantActivation))
			}
		})
	}
}

func TestAvatarManagerFetchesChangedURL(t *testing.T) {
	const source = "upstream:corp"
	queries := &avatarManagerQueries{
		account: db.Account{ID: 7, AvatarSource: pgtype.Text{String: "user", Valid: true}},
		sources: []db.ListAvatarSourcesByAccountRow{{
			Source:      source,
			Etag:        pgtype.Text{String: "old-etag", Valid: true},
			UpstreamUrl: pgtype.Text{String: "https://cdn.test/avatar.png?v=1", Valid: true},
		}},
	}
	manager := NewAvatarManager(queries)
	var fetchedURL string
	manager.fetch = func(_ context.Context, rawURL string, _ bool) ([]byte, error) {
		fetchedURL = rawURL
		return avatarManagerPNG(t), nil
	}

	manager.Refresh(context.Background(), 7, Provider{ID: 11, Slug: "corp"}, AvatarDelivery{URL: "https://cdn.test/avatar.png?v=2"}, nil)

	if fetchedURL != "https://cdn.test/avatar.png?v=2" {
		t.Fatalf("fetched %q, want the new URL", fetchedURL)
	}
	if len(queries.upserts) != 1 || queries.upserts[0].UpstreamUrl.String != "https://cdn.test/avatar.png?v=2" {
		t.Fatalf("upserts = %+v, want one recording the new URL", queries.upserts)
	}
	if len(queries.activations) != 0 {
		t.Fatalf("activations = %d, want the upload to stay active", len(queries.activations))
	}
}

func TestAvatarManagerKeepsStoredAvatarWithoutURL(t *testing.T) {
	queries := &avatarManagerQueries{account: db.Account{ID: 7}}
	manager := NewAvatarManager(queries)
	fetched := false
	manager.fetch = func(context.Context, string, bool) ([]byte, error) {
		fetched = true
		return nil, nil
	}

	manager.Refresh(context.Background(), 7, Provider{ID: 11, Slug: "corp"}, AvatarDelivery{}, nil)

	if fetched || queries.lists != 0 || len(queries.upserts) != 0 || len(queries.activations) != 0 {
		t.Fatalf("empty URL touched avatar state: fetched=%v lists=%d upserts=%d activations=%d",
			fetched, queries.lists, len(queries.upserts), len(queries.activations))
	}
}

func TestAvatarManagerFetchFailureKeepsStoredAvatar(t *testing.T) {
	const source = "upstream:corp"
	queries := &avatarManagerQueries{
		account: db.Account{ID: 7},
		sources: []db.ListAvatarSourcesByAccountRow{{
			Source:      source,
			Etag:        pgtype.Text{String: "etag", Valid: true},
			UpstreamUrl: pgtype.Text{String: "https://cdn.test/avatar.png?v=1", Valid: true},
		}},
	}
	manager := NewAvatarManager(queries)
	manager.logger = slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	manager.fetch = func(context.Context, string, bool) ([]byte, error) {
		return nil, errors.New("upstream unavailable")
	}

	manager.Refresh(context.Background(), 7, Provider{ID: 11, Slug: "corp"}, AvatarDelivery{URL: "https://cdn.test/avatar.png?v=2"}, nil)

	if len(queries.upserts) != 0 || len(queries.activations) != 0 {
		t.Fatalf("failed fetch mutated avatar state: upserts=%d activations=%d", len(queries.upserts), len(queries.activations))
	}
}

func TestAvatarManagerRefreshReturnsAfterTimeout(t *testing.T) {
	queries := &avatarManagerQueries{account: db.Account{ID: 7}}
	manager := NewAvatarManager(queries)
	manager.timeout = 20 * time.Millisecond
	var logs bytes.Buffer
	manager.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	manager.fetch = func(ctx context.Context, _ string, _ bool) ([]byte, error) {
		<-ctx.Done()
		return nil, sanitizeAvatarFetchError(ctx.Err())
	}
	returned := make(chan struct{})

	go func() {
		manager.Refresh(context.Background(), 7, Provider{ID: 11, Slug: "corp"}, AvatarDelivery{URL: "https://cdn.test/avatar.png"}, nil)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh did not return after its timeout")
	}
	if len(queries.upserts) != 0 || len(queries.activations) != 0 {
		t.Fatalf("timed-out fetch mutated avatar state: upserts=%d activations=%d", len(queries.upserts), len(queries.activations))
	}
	if !strings.Contains(logs.String(), "avatar fetch timeout") {
		t.Fatalf("log = %q, want a timeout warning", logs.String())
	}
}

func TestAvatarManagerLogsPersistenceFailuresWithSafeContext(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*avatarManagerQueries)
		message   string
	}{
		{
			name:      "account lookup",
			configure: func(q *avatarManagerQueries) { q.getErr = errors.New("database failed") },
			message:   "account lookup failed",
		},
		{
			name:      "source list",
			configure: func(q *avatarManagerQueries) { q.listErr = errors.New("database failed") },
			message:   "source list failed",
		},
		{
			name:      "source upsert",
			configure: func(q *avatarManagerQueries) { q.upsertErr = errors.New("database failed") },
			message:   "source upsert failed",
		},
		{
			name:      "activation",
			configure: func(q *avatarManagerQueries) { q.activateErr = errors.New("database failed") },
			message:   "activation failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queries := &avatarManagerQueries{account: db.Account{ID: 7}}
			test.configure(queries)
			manager := NewAvatarManager(queries)
			var logs bytes.Buffer
			manager.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			manager.fetch = func(context.Context, string, bool) ([]byte, error) {
				return avatarManagerPNG(t), nil
			}

			manager.Refresh(context.Background(), 7, Provider{
				ID: 11, Slug: "corp", Config: json.RawMessage(`{}`),
			}, AvatarDelivery{URL: "https://private.example/avatar.png"}, nil)

			logged := logs.String()
			for _, want := range []string{
				test.message, `"account_id":7`, `"provider_id":11`, `"provider_slug":"corp"`,
			} {
				if !strings.Contains(logged, want) {
					t.Fatalf("log = %q, want %q", logged, want)
				}
			}
			if strings.Contains(logged, "private.example") {
				t.Fatalf("log leaked avatar URL: %q", logged)
			}
		})
	}
}

func TestAvatarManagerUsesAdapterPrivateNetworkPolicy(t *testing.T) {
	queries := &avatarManagerQueries{account: db.Account{ID: 7}}
	manager := NewAvatarManager(queries)
	manager.logger = slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	var got bool
	manager.fetch = func(_ context.Context, _ string, allowPrivate bool) ([]byte, error) {
		got = allowPrivate
		return nil, nil
	}

	manager.Refresh(context.Background(), 7, Provider{
		ID: 11, Slug: "corp",
	}, AvatarDelivery{URL: "https://cdn.test/avatar.png", AllowPrivateNetwork: true}, nil)

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
