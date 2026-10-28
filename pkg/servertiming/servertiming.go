// Package servertiming reports where a request's time went in a
// Server-Timing response header (https://www.w3.org/TR/server-timing/), so
// the browser's network panel shows it beside each response: the whole
// handler, the database (queries, and the wait for a pooled connection) and
// the key-value store.
//
// Middleware puts a recorder in the request context; the pgx tracer
// (Tracer) and the KV wrapper (KV) add to it from whatever goroutine does the
// work. The header is written with the response headers, so work done after
// the body has started is not counted.
package servertiming

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Metric names, in the order the header lists them.
const (
	metricDB     = "db"
	metricDBWait = "db-wait"
	metricKV     = "kv"
)

var metricOrder = []string{metricDB, metricDBWait, metricKV}

// metricDescriptions names what a metric's count counts.
var metricDescriptions = map[string]string{
	metricDB:     "queries",
	metricDBWait: "connections",
	metricKV:     "calls",
}

type metric struct {
	total time.Duration
	count int
}

// recorder accumulates one request's timings. Database work may run in
// several goroutines at once, hence the lock.
type recorder struct {
	start   time.Time
	mu      sync.Mutex
	metrics map[string]*metric
}

type contextKey struct{}

func fromContext(ctx context.Context) *recorder {
	r, _ := ctx.Value(contextKey{}).(*recorder)
	return r
}

// add records one unit of work of the named kind, if the request is timed.
func add(ctx context.Context, name string, d time.Duration) {
	r := fromContext(ctx)
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.metrics[name]
	if m == nil {
		m = &metric{}
		r.metrics[name] = m
	}
	m.total += d
	m.count++
}

// header renders the Server-Timing value: the whole request so far, then each
// kind of work that happened.
func (r *recorder) header(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := []string{fmt.Sprintf("total;dur=%s", millis(now.Sub(r.start)))}
	for _, name := range metricOrder {
		m := r.metrics[name]
		if m == nil {
			continue
		}
		entries = append(entries, fmt.Sprintf(`%s;dur=%s;desc="%s: %d"`,
			name, millis(m.total), metricDescriptions[name], m.count))
	}
	return strings.Join(entries, ", ")
}

func millis(d time.Duration) string {
	return fmt.Sprintf("%.1f", float64(d)/float64(time.Millisecond))
}

// Middleware times every request it wraps and adds the Server-Timing header to
// its response. Disabled, it returns next unchanged.
func Middleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &recorder{start: time.Now(), metrics: map[string]*metric{}}
			tw := &timingWriter{ResponseWriter: w, rec: rec}
			next.ServeHTTP(tw, r.WithContext(context.WithValue(r.Context(), contextKey{}, rec)))
		})
	}
}

// timingWriter adds the header just before the response headers go out.
type timingWriter struct {
	http.ResponseWriter
	rec     *recorder
	written bool
}

func (w *timingWriter) stamp() {
	if w.written {
		return
	}
	w.written = true
	w.ResponseWriter.Header().Set("Server-Timing", w.rec.header(time.Now()))
}

func (w *timingWriter) WriteHeader(code int) {
	w.stamp()
	w.ResponseWriter.WriteHeader(code)
}

func (w *timingWriter) Write(b []byte) (int, error) {
	w.stamp()
	return w.ResponseWriter.Write(b)
}

// Flush keeps streaming responses working through the wrapper.
func (w *timingWriter) Flush() {
	w.stamp()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *timingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
