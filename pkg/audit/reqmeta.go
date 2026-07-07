package audit

import "context"

type ctxKey int

const (
	ipCtxKey ctxKey = iota
	uaCtxKey
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
