package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/pagination"
	"prohibitorum/pkg/weberr"
)

// humaConfig returns a huma.Config with the project's response transformer
// installed. The transformer stamps the request ID onto *weberr.PublicError
// values before serialization so typed Huma handler errors carry the same
// {code, details?, requestId} envelope as the raw chi handler path. It also
// overrides huma.NewError / huma.NewErrorWithContext so huma's internal
// validation errors (malformed JSON, schema violations) produce the same
// envelope instead of RFC 9457 Problem Details.
func humaConfig() huma.Config {
	// Override huma.NewError so validation errors produce a *PublicError with
	// a registered code — never RFC 9457 message/detail/title/errors shape.
	huma.NewError = newHumaValidationError
	huma.NewErrorWithContext = func(_ huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return newHumaValidationError(status, msg, errs...)
	}
	cfg := huma.DefaultConfig("Prohibitorum Identity API", "1.0.0")
	// Disable the SchemaLinkTransformer (the default CreateHook) so responses
	// do not carry a $schema field — the wire envelope must be exactly
	// {code, details?, requestId} and nothing else.
	cfg.CreateHooks = nil
	// Install the request-ID transformer that stamps RequestID on
	// *PublicError values before serialization.
	cfg.Transformers = append(cfg.Transformers, func(ctx huma.Context, status string, v any) (any, error) {
		if pe, ok := v.(*weberr.PublicError); ok {
			pe.RequestID = weberr.RequestIDFromContext(ctx.Context())
			observeDiagnostic(ctx.Context(), pe.Code, pe.Details)
		}
		return v, nil
	})
	return cfg
}

// newHumaValidationError replaces huma.NewError so every framework error
// produces a *weberr.PublicError with a registered code. The mapping is
// status-aware:
//
//   - 422 (schema validation): validation_failed with safe location/reason
//     details extracted from the first huma.ErrorDetailer. Never echoes raw
//     input values.
//   - 400 (malformed JSON / bad request): bad_request, no details.
//   - 413 (body too large): request_too_large, no details.
//   - 415 (unsupported media type): unsupported_media_type, no details.
//   - >=500 (internal): server_error, no details. Never maps to 422 and
//     never serializes the raw error string.
//
// Non-ErrorDetailer errors are NEVER serialized — their Error() string may
// contain raw input or internal state. Only huma.ErrorDetailer values (which
// carry a curated Message + Location) are used for safe details.
func newHumaValidationError(status int, msg string, errs ...error) huma.StatusError {
	switch {
	case status >= 500:
		// Internal failure — never expose details or raw error text.
		return &weberr.PublicError{Code: "server_error"}
	case status == http.StatusUnprocessableEntity:
		// Schema validation failure — extract safe location/reason from the
		// first ErrorDetailer. Non-ErrorDetailer errors are skipped (their
		// Error() string is never serialized).
		details := map[string]any{}
		for _, e := range errs {
			if e == nil {
				continue
			}
			if detailer, ok := e.(huma.ErrorDetailer); ok {
				ed := detailer.ErrorDetail()
				if ed.Location != "" {
					details["location"] = ed.Location
				}
				if ed.Message != "" {
					details["reason"] = ed.Message
				}
				break
			}
		}
		if len(details) == 0 {
			details = nil
		}
		return &weberr.PublicError{
			Code:    "validation_failed",
			Details: details,
		}
	case status == http.StatusBadRequest:
		return &weberr.PublicError{Code: "bad_request"}
	case status == http.StatusRequestEntityTooLarge:
		return &weberr.PublicError{Code: "request_too_large"}
	case status == http.StatusUnsupportedMediaType:
		return &weberr.PublicError{Code: "unsupported_media_type"}
	default:
		// Any other 4xx without a specific mapping falls back to bad_request.
		if status >= 400 && status < 500 {
			return &weberr.PublicError{Code: "bad_request"}
		}
		// Unknown status — safe fallback.
		return &weberr.PublicError{Code: "server_error"}
	}
}

// writeHumaPublicErr writes a {code, details?, requestId} public-error
// envelope through a huma.Context, bypassing huma's default RFC 9457
// serialization so the wire contract matches the raw chi handler path
// (writeAuthErr). The code is validated against the registry (DefinitionFor);
// the status is taken from the registered definition, not the caller. Details
// are filtered against the definition's DetailKeys whitelist — undeclared
// keys are dropped. An unregistered code falls back to server_error with no
// details. The request ID is read from the context (set by the RequestID
// middleware).
func writeHumaPublicErr(ctx huma.Context, code string, details map[string]any) {
	def, ok := weberr.DefinitionFor(code)
	if !ok {
		def, _ = weberr.DefinitionFor("server_error")
		code = "server_error"
		details = nil
	}
	// Filter details against the definition's whitelist.
	if len(details) > 0 && len(def.DetailKeys) > 0 {
		filtered := make(map[string]any, len(details))
		for k, v := range details {
			if _, allowed := def.DetailKeys[k]; allowed {
				filtered[k] = v
			}
		}
		if len(filtered) == 0 {
			details = nil
		} else {
			details = filtered
		}
	} else if len(def.DetailKeys) == 0 {
		details = nil
	}
	observeDiagnostic(ctx.Context(), code, details)
	requestID := weberr.RequestIDFromContext(ctx.Context())
	ctx.SetHeader("Content-Type", "application/json")
	ctx.SetStatus(def.Status)
	_ = json.NewEncoder(ctx.BodyWriter()).Encode(weberr.PublicError{
		Code:      code,
		Details:   details,
		RequestID: requestID,
	})
}

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
			writeHumaPublicErr(ctx, ae.Code, ae.Details)
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
			writeHumaPublicErr(ctx, ae.Code, ae.Details)
			return
		}
		if !s.hasFreshSudo(sess) {
			ae := authn.ErrSudoRequired()
			writeHumaPublicErr(ctx, ae.Code, ae.Details)
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
				// unexpected error types by routing through writeAuthErr, which
				// logs the detail server-side and returns canonical server_error.
				writeAuthErr(w, err)
				return
			}
			// Use the shared writeAuthErr path so the envelope matches the
			// raw handler path exactly: {code, requestId} with no message.
			writeAuthErr(w, ae)
			return
		}
		h(w, r)
	})
	router.Method(method, path, wrapped)
}

const maxAdminBody = 64 << 10 // 64 KiB

// withAdminBodyControls wraps a raw handler with the two request-shape
// controls every JSON admin mutation needs, independent of sudo policy:
//
//  1. Content-type check: a non-GET request with a body whose Content-Type
//     is present but not application/json is rejected 400 bad_request before
//     the handler runs. A missing Content-Type on a body is also rejected.
//  2. Body-size limit: the body is capped at maxAdminBody (64 KiB).
//
// The size cap is enforced eagerly for unknown-length bodies and lazily for
// advertised-length bodies:
//
//   - If Content-Length is present and exceeds maxAdminBody, reject 413
//     before the handler runs (proactive check).
//   - If Content-Length is -1 (chunked/unknown), the proactive check cannot
//     fire. MaxBytesReader alone is insufficient here: a handler's
//     json.Decode stops at the end of the first valid JSON value and never
//     reads trailing bytes, so an oversized unknown-length body with a valid
//     JSON prefix would slip past the lazy limit and reach the sudo gate /
//     handler. To close that gap we read at most maxAdminBody+1 bytes into
//     memory once, reject 413 if more than maxAdminBody arrived, and restore
//     the buffered bytes as the request body for the handler. This bound is
//     tiny (64 KiB+1) and avoids a second copy: MaxBytesReader would have
//     re-read the same bytes, so we replace it rather than wrap on top.
//   - For known-length bodies ≤ maxAdminBody, MaxBytesReader still guards
//     against a client that lies (sends more than advertised).
//
// Routes through the shared writeBodyTooLarge so every 413 response is
// identical. This wrapper installs BOTH controls exactly once. withFreshSudo
// composes it and then adds the fresh-sudo gate; registerAdminBodyOpHTTP
// uses it directly for intentional admin-only (no-sudo) raw JSON mutation
// routes. Never double-wrap: a route registered through either helper
// already has these controls.
func withAdminBodyControls(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.ContentLength != 0 {
			ct := r.Header.Get("Content-Type")
			if ct == "" || !strings.HasPrefix(ct, "application/json") {
				writeAuthErr(w, authn.ErrBadRequest())
				return
			}
			// Proactive size check: if Content-Length advertises a body
			// larger than the cap, reject 413 before the handler runs.
			if r.ContentLength > maxAdminBody {
				writeBodyTooLarge(w)
				return
			}
			// Unknown-length bodies (ContentLength == -1, e.g. chunked)
			// defeat the proactive check and MaxBytesReader's lazy limit is
			// not enough on its own: json.Decode stops after the first valid
			// JSON value and never reads the trailing bytes, so an oversized
			// unknown-length body with a valid prefix would reach the sudo
			// gate / handler unchecked. Eagerly read at most maxAdminBody+1
			// bytes once; reject 413 if more arrived; otherwise restore the
			// buffered body for the handler. This replaces MaxBytesReader
			// for this request (no avoidable duplicate copy) while still
			// bounding memory to maxAdminBody+1.
			if r.Body != nil && r.ContentLength < 0 {
				buf, err := io.ReadAll(io.LimitReader(r.Body, maxAdminBody+1))
				if err != nil {
					writeBodyTooLarge(w)
					return
				}
				if int64(len(buf)) > maxAdminBody {
					writeBodyTooLarge(w)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(buf))
				r.ContentLength = int64(len(buf))
			}
		}
		if r.Body != nil && r.ContentLength >= 0 {
			// Known-length (including the now-resolved unknown-length case):
			// keep MaxBytesReader as a defense-in-depth guard against a
			// client that sends more than it advertised. The eager read above
			// already capped unknown-length bodies, so this only wraps
			// advertised-length ones.
			r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
		}
		h(w, r)
	}
}

// withFreshSudo wraps a raw handler so the fresh-sudo gate runs as route
// policy, not the handler's first line. Single chokepoint for admin mutations.
//
// Ordering: withAdminBodyControls runs first (content-type check + body-size
// limit). The content-type check short-circuits on a bad type, so a wrong-
// content-type request never reaches the sudo gate. The body-size limit is
// enforced before the handler runs: advertised-length bodies are rejected
// 413 by the proactive Content-Length check, and unknown-length bodies are
// fully read (bounded to maxAdminBody+1) and rejected 413 if oversized —
// so an oversized but correctly-typed request never reaches the gate.
// This is safe: the content-type 400 and body-size 413 reveal nothing about
// grant state (no more informative than the 401 hasFreshSudo would emit).
func (s *Server) withFreshSudo(h http.HandlerFunc) http.HandlerFunc {
	return withAdminBodyControls(func(w http.ResponseWriter, r *http.Request) {
		// Sudo gate: verify fresh grant (pure read — multi-use).
		sess := authn.SessionFromContext(r.Context())
		if s.requireFreshSudo(r.Context(), w, sess) {
			return
		}
		h(w, r)
	})
}

// registerSudoOpHTTP = registerOpHTTP (admin auth check) + withFreshSudo
// (content-type, body-size, fresh-sudo gate). Every sudo-gated admin mutation
// route MUST use this instead of the bare registerOpHTTP so the sudo policy
// cannot drift per-handler.
func (s *Server) registerSudoOpHTTP(router chiRouter, method, path string, req contract.AuthRequirement, h http.HandlerFunc) {
	registerOpHTTP(router, method, path, req, s.withFreshSudo(h))
}

// registerAdminBodyOpHTTP = registerOpHTTP (admin auth check) +
// withAdminBodyControls (content-type, body-size). This is for intentional
// admin-only raw-JSON mutation routes that do NOT require fresh sudo: SAML
// CRUD, app-access management, and group CRUD. Using this helper ensures the
// body + content-type controls cannot drift per-handler, mirroring the
// registerSudoOpHTTP guarantee for the non-sudo tier.
func (s *Server) registerAdminBodyOpHTTP(router chiRouter, method, path string, req contract.AuthRequirement, h http.HandlerFunc) {
	registerOpHTTP(router, method, path, req, withAdminBodyControls(h))
}

// writeCursorInvalidErr writes the canonical pagination_cursor_invalid
// response for a raw HTTP boundary where a cursor failed validation. Any
// pagination error (Codec.Decode failure, ErrCursorInvalid, malformed input)
// routes through here so the response shape cannot drift per-handler: the
// client sees {code, requestId} with HTTP 400 and never the underlying
// crypto / decode detail.
//
// The raw err is logged server-side with the request ID (for correlation) but
// is never serialized; pagination.ErrCursorInvalid intentionally does not
// distinguish tamper vs expiry vs binding mismatch on the wire. Non-cursor
// errors fall through to writeAuthErrForCode with the generic server_error
// code, preserving the existing boundary contract.
func writeCursorInvalidErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, pagination.ErrCursorInvalid) {
		writeAuthErrForCode(w, contract.CodeCursorInvalid, err)
		return
	}
	// Unexpected failure on a cursor path: surface server_error, log raw.
	writeAuthErrForCode(w, "server_error", err)
}
