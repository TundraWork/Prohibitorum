package servertiming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/kv"
)

// serve runs handler behind the middleware and returns the response.
func serve(t *testing.T, enabled bool, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Middleware(enabled)(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	return rec
}

func TestMiddleware_ReportsDatabaseAndKVWork(t *testing.T) {
	store, err := kv.New("memory")
	if err != nil {
		t.Fatal(err)
	}
	timedStore := KV(store)
	rec := serve(t, true, func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var tracer Tracer
		for range 3 {
			end := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{})
			time.Sleep(time.Millisecond)
			tracer.TraceQueryEnd(end, nil, pgx.TraceQueryEndData{})
		}
		acquired := tracer.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
		tracer.TraceAcquireEnd(acquired, nil, pgxpool.TraceAcquireEndData{})
		_ = timedStore.Set(ctx, "k", "v")
		_, _ = timedStore.Get(ctx, "k")
		w.WriteHeader(http.StatusNoContent)
	})

	got := rec.Header().Get("Server-Timing")
	want := regexp.MustCompile(`^total;dur=[0-9.]+, db;dur=[0-9.]+;desc="queries: 3", db-wait;dur=[0-9.]+;desc="connections: 1", kv;dur=[0-9.]+;desc="calls: 2"$`)
	if !want.MatchString(got) {
		t.Fatalf("Server-Timing = %q", got)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMiddleware_StampsOnFirstWriteOnly(t *testing.T) {
	rec := serve(t, true, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("a"))
		// Work after the headers went out cannot change them.
		var tracer Tracer
		tracer.TraceQueryEnd(tracer.TraceQueryStart(r.Context(), nil, pgx.TraceQueryStartData{}), nil, pgx.TraceQueryEndData{})
		_, _ = w.Write([]byte("b"))
	})
	if got := rec.Header().Values("Server-Timing"); len(got) != 1 || !regexp.MustCompile(`^total;dur=[0-9.]+$`).MatchString(got[0]) {
		t.Fatalf("Server-Timing = %q", got)
	}
	if rec.Body.String() != "ab" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestMiddleware_DisabledAddsNothing(t *testing.T) {
	rec := serve(t, false, func(w http.ResponseWriter, r *http.Request) {
		if fromContext(r.Context()) != nil {
			t.Error("a disabled middleware must not time the request")
		}
		w.WriteHeader(http.StatusOK)
	})
	if rec.Header().Get("Server-Timing") != "" {
		t.Fatalf("Server-Timing = %q", rec.Header().Get("Server-Timing"))
	}
}

func TestTracerAndKV_OutsideARequestDoNothing(t *testing.T) {
	ctx := context.Background()
	var tracer Tracer
	if got := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{}); got != ctx {
		t.Fatal("an untimed query must keep its context")
	}
	store, _ := kv.New("memory")
	if err := KV(store).Set(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	if v, err := KV(store).Get(ctx, "k"); err != nil || v != "v" {
		t.Fatalf("Get = %q, %v", v, err)
	}
}

func TestRecorder_ConcurrentWork(t *testing.T) {
	rec := serve(t, true, func(w http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				add(r.Context(), metricDB, time.Microsecond)
			}()
		}
		wg.Wait()
		w.WriteHeader(http.StatusOK)
	})
	if !regexp.MustCompile(`db;dur=[0-9.]+;desc="queries: 50"`).MatchString(rec.Header().Get("Server-Timing")) {
		t.Fatalf("Server-Timing = %q", rec.Header().Get("Server-Timing"))
	}
}
