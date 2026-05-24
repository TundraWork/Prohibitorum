package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"prohibitorum/pkg/auth"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/errorx"
)

// registerOp wraps huma.Register so every operation declares its auth
// requirement at the call site. The wrapper appends a per-operation
// middleware that reads *auth.Session from the request context (placed
// there by auth.LoadSession on the chi router) and calls auth.Check
// before invoking the handler. On failure, the canonical AuthError is
// written via huma.WriteErr — clients see {code, message} in the body
// and the correct HTTP status.
func registerOp[I, O any](
	api huma.API,
	op huma.Operation,
	handler func(context.Context, *I) (*O, error),
	req contract.AuthRequirement,
) {
	if req.Kind != contract.AuthPublic {
		// Public ops omit the security requirement in OpenAPI; everyone else
		// references the prohibitorumSession scheme (registered at server boot).
		op.Security = append(op.Security, map[string][]string{
			"prohibitorumSession": {},
		})
	}
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		sess := auth.SessionFromContext(ctx.Context())
		if err := auth.Check(sess, req); err != nil {
			ae := auth.AsAuthError(err)
			_ = huma.WriteErr(api, ctx, ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
			return
		}
		next(ctx)
	})
	huma.Register(api, op, handler)
}

// registerOpHTTP wraps a raw chi handler with the same auth.Check gate.
// Used for endpoints that need to write Set-Cookie headers and read
// streaming/JSON request bodies — Huma's typed I/O doesn't accommodate
// cookie writes ergonomically. The trade-off is no OpenAPI doc for these
// routes; document them in api.md manually.
//
// router is anything that exposes the chi method we need. We accept the
// minimal interface so this helper composes with chi.Mux or a sub-router.
type chiRouter interface {
	Method(method, pattern string, h http.Handler)
}

func registerOpHTTP(
	router chiRouter,
	method, path string,
	req contract.AuthRequirement,
	h http.HandlerFunc,
) {
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := auth.SessionFromContext(r.Context())
		if err := auth.Check(sess, req); err != nil {
			ae := auth.AsAuthError(err)
			if ae == nil {
				// auth.Check should only return AuthErrors, but guard against
				// unexpected error types to avoid a nil-deref on ae.Status.
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			errResp := huma.NewError(ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(errResp.GetStatus())
			_ = json.NewEncoder(w).Encode(errResp)
			return
		}
		h(w, r)
	})
	router.Method(method, path, wrapped)
}
