package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/weberr"
)

// ceremonyTTL is how long a /begin's KV stash survives before /complete must claim it.
const ceremonyTTL = 5 * time.Minute

// newCeremonyToken returns a URL-safe random token for the ceremony cookie.
func newCeremonyToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ceremony token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sessionView projects a db.Account into the response shape of GET /me and
// POST /auth/login/complete. origin is s.config.PublicOrigins[0] (or "").
func (s *Server) sessionView(a *db.Account) contract.SessionView {
	v := contract.SessionView{
		ID:          a.ID,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		Role:        a.Role,
		Attributes:  decodeAttributes(a.Attributes),
	}
	origin := ""
	if s.config != nil && len(s.config.PublicOrigins) > 0 {
		origin = s.config.PublicOrigins[0]
	}
	if u := avatar.AccountURL(*a, origin); u != "" {
		v.AvatarURL = &u
	}
	if a.AvatarSource.Valid {
		src := a.AvatarSource.String
		v.AvatarSource = &src
	}
	if origin != "" {
		// avatarSourceUrls is best-effort: a failed list read degrades gracefully
		// (the field is simply omitted) rather than failing the whole /me response.
		if rows, lerr := s.avatarQ().ListAvatarSourcesByAccount(context.Background(), a.ID); lerr == nil && len(rows) > 0 {
			urls := make(map[string]string, len(rows))
			labels := make(map[string]string, len(rows))
			for _, row := range rows {
				if u := avatar.SourceURL(a.OidcSubject.String(), row.Source, row.Etag.String, origin); u != "" {
					urls[row.Source] = u
					// Label upstream sources with their IdP display name so the
					// picker shows "Google" etc. instead of "upstream:google".
					if row.IdpDisplayName != "" {
						labels[row.Source] = row.IdpDisplayName
					}
				}
			}
			if len(urls) > 0 {
				v.AvatarSourceUrls = urls
			}
			if len(labels) > 0 {
				v.AvatarSourceLabels = labels
			}
		}
	}
	return v
}

// canonicalServerError is the stable code returned to clients when
// writeAuthErr receives a non-AuthError (a real internal failure: DB, KV,
// crypto). The raw error is logged server-side with the request ID and a
// curated set of safe fields; the client only sees code "server_error" and
// HTTP 500 — never the underlying error string, which may carry connection
// strings or stack detail.
const canonicalServerError = "server_error"

// writeBodyTooLarge writes the canonical 413 response for an oversized admin
// body. Both the proactive path (withAdminBodyControls Content-Length check)
// and the lazy path (writeAuthErr detecting *http.MaxBytesError) route through
// here, so the response shape cannot drift between them. Uses the public-error
// envelope {code, requestId} — no message field. The registry authoritatively
// maps "request_too_large" to HTTP 413.
func writeBodyTooLarge(w http.ResponseWriter) {
	weberr.WriteJSON(w, "request_too_large", nil, w.Header().Get(weberr.HeaderRequestID))
}

// writeAuthErr serializes an *authn.AuthError onto a raw http.ResponseWriter
// using the project's public-error envelope: {code, details?, requestId}.
// When the AuthError carries a RetryAfter duration (rate-limit, factor
// lockout), the header is emitted as integer seconds, rounded up so a
// sub-second remainder still nudges the client past the lockout boundary.
//
// The response's X-Request-ID header is set by the RequestID middleware
// (installed before session/auth routing); writeAuthErr reads it back so the
// JSON body's requestId matches the header. When the middleware did not run
// (e.g. a unit test calling writeAuthErr directly), requestId is empty but
// the field is still present in the JSON.
//
// Non-AuthError values (DB/KV/crypto failures) are NEVER surfaced to the
// client. They are logged at WARN with the request ID, registered code, and a
// curated safe category — never WithError(err) which would log the raw
// error string (connection strings, query text, stack fragments) to the same
// line. The response carries only {code:"server_error", requestId}.
func writeAuthErr(w http.ResponseWriter, err error) {
	requestID := w.Header().Get(weberr.HeaderRequestID)
	// Detect an oversized admin body: MaxBytesReader (installed by
	// withAdminBodyControls) wraps r.Body; when the handler's json.Decode
	// reads past the limit, the error chains through wrapping as
	// *http.MaxBytesError. Map it to 413 via the shared writer so the lazy
	// path matches the proactive path exactly.
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeBodyTooLarge(w)
		return
	}
	ae := authn.AsAuthError(err)
	if ae == nil {
		// Internal (non-domain) error: log with request ID + curated fields
		// only — never WithError(err), which would persist the raw error
		// string (may contain secrets) to structured logs. A nil err also
		// lands here.
		logInternalError(requestID, canonicalServerError, err)
		weberr.WriteJSON(w, canonicalServerError, nil, requestID)
		return
	}
	if ae.RetryAfter > 0 {
		secs := int((ae.RetryAfter + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
	}
	weberr.WriteJSON(w, ae.Code, ae.Details, requestID)
}

// logInternalError emits a curated log line for an internal (non-domain)
// error. It logs the request ID (so operators can correlate the public
// response to structured records), the registered code passed by the
// caller (e.g. "database_unavailable", "kv_unavailable",
// "ceremony_internal_error", or "server_error" for the generic fallback),
// and a safe diagnostic category. It deliberately does NOT call
// WithError(err) — err.Error() may contain connection strings, query text,
// or stack fragments that must not be persisted to structured logs.
// Callers that need the raw detail for debugging should add a separate
// debug-level entry with an explicit, reviewed field if warranted.
func logInternalError(requestID, code string, err error) {
	entry := logrus.WithField("code", code).
		WithField("category", "internal").
		WithField("request_id", requestID)
	if err == nil {
		entry.Warn("internal error (nil)")
		return
	}
	// Determine a safe diagnostic kind from the error type — never log err.Error().
	entry = entry.WithField("error_type", errorTypeLabel(err))
	entry.Warn("internal error")
}

// errorTypeLabel returns a short, safe diagnostic label for an error value
// suitable for structured logs. It inspects the error's concrete type (via
// errors.As) — never err.Error() — so connection strings and stack fragments
// in the error message are never persisted.
func errorTypeLabel(err error) string {
	if err == nil {
		return "nil"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "pg_" + pgErr.Code
	}
	if errors.Is(err, kv.ErrKeyNotFound) {
		return "kv_key_not_found"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "net"
	}
	if errors.Is(err, context.Canceled) {
		return "ctx_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ctx_deadline"
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return "max_bytes"
	}
	return "other"
}

// writeAuthErrForCode writes a registered public-error envelope for a known
// code, used at raw HTTP boundaries where the caller knows the operation-
// specific internal code (e.g. "kv_unavailable") rather than collapsing every
// unexpected failure to generic "server_error". The code MUST be registered;
// if it is not, WriteJSON falls back to server_error. The raw err is logged
// with the request ID, the selected registered code, and curated fields
// only (never WithError) — so the response code and log code correlate via
// the shared request ID.
func writeAuthErrForCode(w http.ResponseWriter, code string, err error) {
	requestID := w.Header().Get(weberr.HeaderRequestID)
	logInternalError(requestID, code, err)
	weberr.WriteJSON(w, code, nil, requestID)
}

// redirectAuthErrToError sends a browser-navigated flow error to the SPA
// /error page instead of writing JSON. Use ONLY on full-page (redirect-target)
// handlers — federation login/callback, identity link, invite start. The
// AuthError code drives the SPA message; a fresh ref is returned so the caller
// can stamp it onto an existing audit row. Falls back to "server_error".
func redirectAuthErrToError(w http.ResponseWriter, r *http.Request, err error) string {
	return redirectAuthErrToErrorReturn(w, r, err, "")
}

// redirectAuthErrToErrorReturn is redirectAuthErrToError with a return_to hint
// so the /error page's "go back" link can send the user where they started
// (e.g. /connected for an identity-link begin). Pass only a server-validated,
// same-origin returnTo (e.g. the value from validateFederationReturnTo); "" to
// omit it.
func redirectAuthErrToErrorReturn(w http.ResponseWriter, r *http.Request, err error, returnTo string) string {
	code := "server_error"
	ae := authn.AsAuthError(err)
	if ae != nil {
		code = ae.Code
	}
	ref := weberr.NewRef()
	// Correlate the user-facing ref with the cause. A non-AuthError is a real
	// server-side failure → log it at warn with the ref, code, request ID, and
	// a safe error_type label — never WithError(err), which would persist the
	// raw error string (may contain secrets) to structured logs. An AuthError
	// is an expected outcome (bad_credentials, link_required, …) → debug.
	requestID := weberr.RequestIDFromContext(r.Context())
	entry := logx.WithContext(r.Context()).
		WithField("ref", ref).
		WithField("code", code).
		WithField("request_id", requestID)
	if ae == nil {
		if err != nil {
			entry = entry.WithField("error_type", errorTypeLabel(err))
		}
		entry.Warn("auth error redirect")
	} else {
		entry.Debug("auth error redirect")
	}
	weberr.RedirectToErrorWithReturn(w, r, code, ref, returnTo)
	return ref
}

// matchCredentialRowID finds the db.WebauthnCredential row whose CredentialID
// equals the raw credential ID returned by the authenticator. Returns 0 if not
// found (should never happen — FinishPasskeyLogin already verified membership).
func matchCredentialRowID(creds []db.WebauthnCredential, rawID []byte) int32 {
	for _, c := range creds {
		if bytes.Equal(c.CredentialID, rawID) {
			return c.ID
		}
	}
	return 0
}

// ----- GET /auth/status (typed) --------------------------------------------

type authStatusOut struct {
	Body contract.AuthStatus
}

func (s *Server) handleAuthStatus(ctx context.Context, _ *struct{}) (*authStatusOut, error) {
	bootstrapped, err := s.queries.HasAnyActiveAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth status: %w", err)
	}
	return &authStatusOut{Body: contract.AuthStatus{Bootstrapped: bootstrapped}}, nil
}

// ----- POST /auth/login/begin (raw chi) ------------------------------------

func (s *Server) handleLoginBeginHTTP(w http.ResponseWriter, r *http.Request) {
	// Per-IP cap on the unauthenticated login ceremony — bounds ceremony-spam
	// (KV writes) and WebAuthn-verification cost (audit SESS-3).
	if s.rateLimit(w, r, "login:ip:"+s.clientIP.IP(r), loginIPLimit, authIPWindow) {
		return
	}

	bootstrapped, err := s.queries.HasAnyActiveAdmin(r.Context())
	if err != nil {
		writeAuthErrForCode(w, "database_unavailable", fmt.Errorf("login/begin: %w", err))
		return
	}
	if !bootstrapped {
		writeAuthErr(w, authn.ErrNotBootstrapped())
		return
	}

	assertion, sessionData, err := s.webauthn.BeginDiscoverableLogin(webauthnauth.LoginOptions()...)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
		return
	}

	token, err := newCeremonyToken()
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	payload, err := json.Marshal(sessionData)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("login/begin marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), "webauthn_ceremony:login:"+token, string(payload), ceremonyTTL); err != nil {
		writeAuthErrForCode(w, "kv_unavailable", fmt.Errorf("login/begin setex: %w", err))
		return
	}

	http.SetCookie(w, sessstore.CeremonyCookie(s.config, r, token))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(assertion.Response)
}

// ----- POST /auth/login/complete (raw chi) ---------------------------------

func (s *Server) handleLoginCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	// Per-IP cap on the login ceremony — shares the begin budget so a ceremony
	// (begin+complete) counts together; bounds WebAuthn-verify + DB cost on the
	// unauthenticated surface (audit SESS-3).
	if s.rateLimit(w, r, "login:ip:"+s.clientIP.IP(r), loginIPLimit, authIPWindow) {
		return
	}

	cer, err := r.Cookie(sessstore.CeremonyCookieName)
	if err != nil || cer.Value == "" {
		writeAuthErr(w, authn.ErrCeremonyMissing())
		return
	}
	// Pop the ceremony stash atomically: single-use prevents an attacker
	// who has captured a WebAuthn assertion (e.g. via a proxy / replay)
	// from completing the login twice against the same challenge state.
	// The pre-bundle Get-then-Del race let two concurrent calls both
	// observe the value; one would issue a session and the other would
	// retry FinishPasskeyLogin with the same SessionData (which would
	// likely fail authenticator-side checks but constituted a wider
	// attack surface than necessary). On a race the loser sees
	// ErrKeyNotFound → ceremony_expired, same UX as a TTL miss.
	raw, err := s.kvStore.Pop(r.Context(), "webauthn_ceremony:login:"+cer.Value)
	if err != nil {
		// kv.ErrKeyNotFound or wrapped — both surface as expired ceremony.
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "ceremony_state_missing",
			"client_ip": s.clientIP.IP(r),
		}).Warn("auth")
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			Factor: audit.FactorWebAuthn,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "ceremony_missing"},
		})
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sessionData); err != nil {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "ceremony_state_corrupt",
			"client_ip": s.clientIP.IP(r),
		}).Warn("auth")
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			Factor: audit.FactorWebAuthn,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "ceremony_corrupt"},
		})
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	// FinishPasskeyLogin resolves the user via our handler and verifies the
	// assertion. The handler is called with (rawID, userHandle); we look up
	// the account by webauthn_user_handle and return a WebAuthnAccount adapter.
	var resolvedAccount db.Account
	var resolvedCreds []db.WebauthnCredential
	handler := func(_ /*rawID*/, userHandle []byte) (webauthn.User, error) {
		a, err := s.queries.GetAccountByWebauthnUserHandle(r.Context(), userHandle)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("unknown user handle")
			}
			return nil, err
		}
		creds, err := s.queries.ListCredentialsByAccount(r.Context(), a.ID)
		if err != nil {
			return nil, err
		}
		resolvedAccount = a
		resolvedCreds = creds
		return &webauthnauth.WebAuthnAccount{Account: &a, Credentials: creds}, nil
	}

	_, credential, err := s.webauthn.FinishPasskeyLogin(handler, sessionData, r)
	if err != nil {
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			Factor: audit.FactorWebAuthn,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "finish_failed"},
		})
		writeAuthErr(w, webauthnauth.MapLoginCeremonyError(r.Context(), err))
		return
	}
	if resolvedAccount.ID == 0 {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":     "auth.login_failure",
			"reason":    "no_account",
			"client_ip": s.clientIP.IP(r),
		}).Warn("auth")
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			Factor: audit.FactorWebAuthn,
			Event:  audit.EventFail,
			Detail: map[string]any{"reason": "no_account"},
		})
		writeAuthErr(w, authn.ErrLoginAccountNotFound())
		return
	}
	if resolvedAccount.Disabled {
		logx.WithContext(r.Context()).WithFields(logrus.Fields{
			"event":      "auth.login_failure",
			"reason":     "account_disabled",
			"account_id": resolvedAccount.ID,
			"client_ip":  s.clientIP.IP(r),
		}).Warn("auth")
		accountID := resolvedAccount.ID
		audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
			AccountID: &accountID,
			Factor:    audit.FactorWebAuthn,
			Event:     audit.EventFail,
			Detail:    map[string]any{"reason": "account_disabled"},
		})
		writeAuthErr(w, authn.ErrAccountDisabled())
		return
	}
	if me := s.maintenanceLockout(r.Context(), resolvedAccount.ID); me != nil {
		writeAuthErr(w, me)
		return
	}

	// Update credential usage (sign_count + last_used_at), then issue session.
	// Check for sign-count regression (potential cloned authenticator) before
	// updating — stamp clone_warning_at on the credential row if detected.
	// Do not reject the login; the stamp is for admin forensics.
	credRowID := matchCredentialRowID(resolvedCreds, credential.ID)
	if credRowID != 0 {
		// Find the old sign count from the resolved creds list.
		newCount := int64(credential.Authenticator.SignCount)
		for _, c := range resolvedCreds {
			if c.ID == credRowID && newCount < c.SignCount {
				_ = s.queries.SetCredentialCloneWarning(r.Context(), credRowID)
				logx.WithContext(r.Context()).WithFields(logrus.Fields{
					"event":         "auth.clone_warning",
					"account_id":    resolvedAccount.ID,
					"credential_id": credRowID,
					"old_count":     c.SignCount,
					"new_count":     newCount,
				}).Warn("auth")
				cloneAccountID := resolvedAccount.ID
				cloneCredRef := int64(credRowID)
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					AccountID:     &cloneAccountID,
					Factor:        audit.FactorWebAuthn,
					Event:         audit.EventCloneWarning,
					CredentialRef: &cloneCredRef,
					Detail:        map[string]any{"reason": "clone_warning"},
				})
				break
			}
		}
		_ = s.queries.UpdateCredentialUsage(r.Context(), db.UpdateCredentialUsageParams{
			ID:        credRowID,
			AccountID: resolvedAccount.ID,
			SignCount: newCount,
		})
	}
	// Ceremony stash was Popped atomically above; no Del needed here.
	http.SetCookie(w, sessstore.ClearedCeremonyCookie(s.config, r))

	ip := s.clientIP.IP(r)
	token, _, err := s.sessionStore.Issue(r.Context(), resolvedAccount.ID, ip, r.UserAgent(), []string{"hwk"}, nil)
	if err != nil {
		writeAuthErrForCode(w, "ceremony_internal_error", fmt.Errorf("session issue: %w", err))
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, resolvedAccount.ID, token, s.config.SessionTTL))

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.login_success",
		"account_id": resolvedAccount.ID,
		"username":   resolvedAccount.Username,
		"client_ip":  s.clientIP.IP(r),
	}).Info("auth")

	successAccountID := resolvedAccount.ID
	var successCredRef *int64
	if credRowID != 0 {
		ref := int64(credRowID)
		successCredRef = &ref
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID:     &successAccountID,
		Factor:        audit.FactorWebAuthn,
		Event:         audit.EventUse,
		CredentialRef: successCredRef,
		Detail:        map[string]any{"reason": "login"},
	})
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &successAccountID,
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionStart,
		Detail:    map[string]any{"via": "webauthn"},
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.LoginResult{
		Redirect: validateReturnTo(r.URL.Query().Get("return_to"), s.config),
	})
}

// ----- POST /auth/logout (raw chi) -----------------------------------------

func (s *Server) handleLogoutHTTP(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
		if id, tok, ok := sessstore.ParseCookieValue(c.Value); ok {
			_ = s.sessionStore.Revoke(r.Context(), id, tok)
			logx.WithContext(r.Context()).WithFields(logrus.Fields{
				"event":      "auth.logout",
				"account_id": id,
				"client_ip":  s.clientIP.IP(r),
			}).Info("auth")
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				AccountID: &id,
				Factor:    audit.FactorSession,
				Event:     audit.EventSessionEnd,
				Detail:    map[string]any{"reason": "logout"},
			})
		}
	}
	http.SetCookie(w, sessstore.ClearedSessionCookie(s.config, r))
	w.WriteHeader(http.StatusNoContent)
}
