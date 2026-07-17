package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/diagnostic"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/weberr"
)

type diagnosticCaptureKey struct{}

type diagnosticCapture struct {
	code   string
	fields map[string]any
}

func (c *diagnosticCapture) observe(code string, fields map[string]any) {
	if c.code != "" {
		return
	}
	c.code = code
	if len(fields) != 0 {
		c.fields = make(map[string]any, len(fields))
		for key, value := range fields {
			c.fields[key] = value
		}
	}
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	capture *diagnosticCapture
}

func (w *diagnosticResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *diagnosticResponseWriter) ObservePublicError(code string, fields map[string]any) {
	w.capture.observe(code, fields)
}

func observeDiagnostic(ctx context.Context, code string, fields map[string]any) {
	if capture, ok := ctx.Value(diagnosticCaptureKey{}).(*diagnosticCapture); ok {
		capture.observe(code, fields)
	}
}

func diagnosticCaptureMW(store diagnostic.StoreWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capture := &diagnosticCapture{}
			ctx := context.WithValue(r.Context(), diagnosticCaptureKey{}, capture)
			r = r.WithContext(ctx)
			next.ServeHTTP(&diagnosticResponseWriter{ResponseWriter: w, capture: capture}, r)
			if capture.code == "" {
				return
			}

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			def, _ := weberr.DefinitionFor(capture.code)
			record := diagnostic.Record{
				RequestID: weberr.RequestIDFromContext(r.Context()),
				Code:      capture.code,
				Operation: r.Method + " " + route,
				Method:    r.Method,
				Route:     route,
				Retryable: def.Retryable,
				Fields:    capture.fields,
			}
			if session := authn.SessionFromContext(r.Context()); session != nil && session.Account != nil {
				record.AccountID = &session.Account.ID
			}
			writeCtx := context.WithoutCancel(r.Context())
			if err := store.Record(writeCtx, record); err != nil {
				logx.WithContext(writeCtx).Warn("diagnostic: record failed")
			}
		})
	}
}
