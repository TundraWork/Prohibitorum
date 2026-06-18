package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/errorx"
)

// registerOp wraps huma.Register so every operation declares its auth
// requirement at the call site. The wrapper appends a per-operation
// middleware that reads *auth.Session from the request context (placed
// there by auth.LoadSession on the chi router) and calls authn.Check
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
		sess := authn.SessionFromContext(ctx.Context())
		if err := authn.Check(sess, req); err != nil {
			ae := authn.AsAuthError(err)
			_ = huma.WriteErr(api, ctx, ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
			return
		}
		next(ctx)
	})
	huma.Register(api, op, handler)
}

// registerSudoOp is registerOp plus a fresh-sudo gate. It is for typed Huma
// admin MUTATIONS (account/invitation lifecycle) that would otherwise need a
// raw-HTTP rewrite to reach registerSudoOpHTTP — this keeps their typed I/O +
// OpenAPI docs while still requiring step-up auth. The sudo check runs as an
// operation middleware AFTER the auth check and routes through
// s.hasFreshSudo (the single chokepoint shared with the raw withFreshSudo
// path), writing ErrSudoRequired via huma on absence. It is a free function
// (not a method) because Go methods cannot be generic; pass the Server.
func registerSudoOp[I, O any](
	s *Server,
	api huma.API,
	op huma.Operation,
	handler func(context.Context, *I) (*O, error),
	req contract.AuthRequirement,
) {
	if req.Kind != contract.AuthPublic {
		op.Security = append(op.Security, map[string][]string{
			"prohibitorumSession": {},
		})
	}
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		sess := authn.SessionFromContext(ctx.Context())
		if err := authn.Check(sess, req); err != nil {
			ae := authn.AsAuthError(err)
			_ = huma.WriteErr(api, ctx, ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
			return
		}
		if !s.hasFreshSudo(sess) {
			ae := authn.ErrSudoRequired()
			_ = huma.WriteErr(api, ctx, ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
			return
		}
		next(ctx)
	})
	huma.Register(api, op, handler)
}

// registerOpHTTP wraps a raw chi handler with the same authn.Check gate.
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
		sess := authn.SessionFromContext(r.Context())
		if err := authn.Check(sess, req); err != nil {
			ae := authn.AsAuthError(err)
			if ae == nil {
				// authn.Check should only return AuthErrors, but guard against
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

const maxAdminBody = 64 << 10 // 64 KiB

// withFreshSudo wraps a raw handler so the fresh-sudo gate runs as route
// policy, not the handler's first line. Single chokepoint for admin mutations.
//
// Ordering: content-type + body-size checks run BEFORE requireFreshSudo so a
// malformed request never touches the sudo gate. This is safe: the
// content-type check reveals nothing about grant state (a 400 is no more
// informative than the 401 that requireFreshSudo would have emitted).
func (s *Server) withFreshSudo(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Baseline body checks first — do not consume the sudo grant on a
		// clearly malformed request.
		if r.Method != http.MethodGet && r.ContentLength != 0 {
			ct := r.Header.Get("Content-Type")
			if ct != "" && !strings.HasPrefix(ct, "application/json") {
				writeAuthErr(w, authn.ErrBadRequest())
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
		}
		// Sudo gate: verify fresh grant (pure read — multi-use).
		sess := authn.SessionFromContext(r.Context())
		if s.requireFreshSudo(r.Context(), w, sess) {
			return
		}
		h(w, r)
	}
}

// registerSudoOpHTTP = registerOpHTTP (admin auth check) + withFreshSudo
// (content-type, body-size, fresh-sudo gate). Every admin mutation route MUST
// use this instead of the bare registerOpHTTP so the sudo policy cannot drift
// per-handler.
func (s *Server) registerSudoOpHTTP(router chiRouter, method, path string, req contract.AuthRequirement, h http.HandlerFunc) {
	registerOpHTTP(router, method, path, req, s.withFreshSudo(h))
}
