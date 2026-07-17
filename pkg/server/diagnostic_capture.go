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

var diagnosticHTTPMethods = [...]string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodConnect,
	http.MethodOptions,
	http.MethodTrace,
}

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

			routeCtx := chi.RouteContext(r.Context())
			route := routeCtx.RoutePattern()
			if route == "" && routeCtx.Routes != nil {
				routePath := r.URL.RawPath
				if routePath == "" {
					routePath = r.URL.Path
				}
				if routePath == "" {
					routePath = "/"
				}
				method := canonicalDiagnosticMethod(r.Method)
				if method != "OTHER" {
					matchedCtx := chi.NewRouteContext()
					if routeCtx.Routes.Match(matchedCtx, method, routePath) {
						route = matchedCtx.RoutePattern()
					}
				} else {
					for _, candidate := range diagnosticHTTPMethods {
						matchedCtx := chi.NewRouteContext()
						if routeCtx.Routes.Match(matchedCtx, candidate, routePath) {
							route = matchedCtx.RoutePattern()
							break
						}
					}
				}
			}
			if route == "" {
				route = "unmatched"
			}
			method := canonicalDiagnosticMethod(r.Method)
			def, _ := weberr.DefinitionFor(capture.code)
			record := diagnostic.Record{
				RequestID: weberr.RequestIDFromContext(r.Context()),
				Code:      capture.code,
				Operation: method + " " + route,
				Method:    method,
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

func canonicalDiagnosticMethod(method string) string {
	switch method {
	case http.MethodGet:
		return http.MethodGet
	case http.MethodHead:
		return http.MethodHead
	case http.MethodPost:
		return http.MethodPost
	case http.MethodPut:
		return http.MethodPut
	case http.MethodPatch:
		return http.MethodPatch
	case http.MethodDelete:
		return http.MethodDelete
	case http.MethodConnect:
		return http.MethodConnect
	case http.MethodOptions:
		return http.MethodOptions
	case http.MethodTrace:
		return http.MethodTrace
	default:
		return "OTHER"
	}
}
