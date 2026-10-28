package servertiming

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tracer adds each database query, and each wait for a pooled connection, to
// the timing of the request whose context it ran in. Set it as the pool's
// ConnConfig.Tracer; pgxpool also uses it as its acquire tracer.
type Tracer struct{}

var (
	_ pgx.QueryTracer       = Tracer{}
	_ pgxpool.AcquireTracer = Tracer{}
)

type startKey struct{ kind string }

func started(ctx context.Context, kind string) context.Context {
	if fromContext(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, startKey{kind}, time.Now())
}

func finished(ctx context.Context, kind, metric string) {
	if start, ok := ctx.Value(startKey{kind}).(time.Time); ok {
		add(ctx, metric, time.Since(start))
	}
}

func (Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return started(ctx, "query")
}

func (Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	finished(ctx, "query", metricDB)
}

func (Tracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return started(ctx, "acquire")
}

func (Tracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	finished(ctx, "acquire", metricDBWait)
}
