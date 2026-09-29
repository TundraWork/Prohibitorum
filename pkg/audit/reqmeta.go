package audit

import "context"

type ctxKey int

const (
	ipCtxKey ctxKey = iota
	uaCtxKey
	patCtxKey
)

// WithRequestMeta returns a ctx carrying the client IP + User-Agent for audit
// enrichment. The RequestMeta middleware sets it once per request; dbWriter.Record
// reads it back to auto-fill Record.IP/UserAgent when a call site left them empty.
func WithRequestMeta(ctx context.Context, ip, ua string) context.Context {
	if ip != "" {
		ctx = context.WithValue(ctx, ipCtxKey, ip)
	}
	if ua != "" {
		ctx = context.WithValue(ctx, uaCtxKey, ua)
	}
	return ctx
}

func ipFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ipCtxKey).(string); ok {
		return v
	}
	return ""
}

func uaFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(uaCtxKey).(string); ok {
		return v
	}
	return ""
}

// WithPATActor marks ctx as acting through the Personal Access Token with the
// given id. dbWriter.Record adds it to Detail as pat_id, so every event a
// token causes traces back to that token. The key lives here because authn
// already imports audit, not the reverse.
func WithPATActor(ctx context.Context, patID int32) context.Context {
	return context.WithValue(ctx, patCtxKey, patID)
}

func patFromCtx(ctx context.Context) (int32, bool) {
	v, ok := ctx.Value(patCtxKey).(int32)
	return v, ok
}
