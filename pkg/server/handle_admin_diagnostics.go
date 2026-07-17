// Package server — handle_admin_diagnostics.go
//
// Admin request-diagnostic lookup:
//
//	GET /api/prohibitorum/diagnostics/{requestId}
//
// Requires admin + fresh sudo (enforced by registerSudoOpHTTP at route
// registration), enforces a per-account rate limit, emits an audit event,
// and performs exact-ID lookup only — no enumeration. Expired or absent
// records return 404.
//
// The response body is the curated diagnostic record: {requestId, code,
// operation, method, route, retryable, fields, occurredAt, expiresAt}.
// Raw cause text, secrets, headers, and tokens are never present — the
// diagnostic store validates every field key against the weberr registry
// before insert, so the table only contains registry-approved fields.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/diagnostic"
)

// diagnosticLookupLimit is the per-account rate cap for diagnostic lookups.
// An admin may look up at most 20 request IDs per minute — enough for
// incident investigation, not enough for enumeration.
const (
	diagnosticLookupLimit  = 20
	diagnosticLookupWindow = time.Minute
)

// diagnosticView is the wire-safe projection of a diagnostic record. It
// contains only curated fields — never raw cause text, secrets, or tokens.
type diagnosticView struct {
	RequestID  string         `json:"requestId"`
	Code       string         `json:"code"`
	Operation  string         `json:"operation"`
	Method     string         `json:"method"`
	Route      string         `json:"route"`
	Retryable  bool           `json:"retryable"`
	Fields     map[string]any `json:"fields,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

// handleAdminDiagnosticLookupHTTP handles exact-ID diagnostic record lookup.
// The route is registered via registerSudoOpHTTP (admin + fresh sudo gate)
// and additionally enforces a per-account rate limit before the DB lookup.
//
// No list/bulk endpoint exists — this is the only diagnostic access path.
func (s *Server) handleAdminDiagnosticLookupHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Account == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}

	// Per-account rate limit: bounds the lookup surface so an admin (or a
	// compromised admin session) cannot enumerate request IDs at scale.
	rlKey := diagnosticRateLimitKey(sess.Account.ID)
	if !s.rateLimiter.Allow(rlKey, diagnosticLookupLimit, diagnosticLookupWindow) {
		ae := authn.ErrRateLimited()
		ae.RetryAfter = s.rateLimiter.RetryAfter(rlKey)
		writeAuthErr(w, ae)
		return
	}

	requestID := chi.URLParam(r, "requestId")
	if requestID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	rec, err := s.diagStore.Lookup(r.Context(), requestID)
	if err != nil {
		if errors.Is(err, diagnostic.ErrNotFound) {
			writeAuthErr(w, authn.ErrDiagnosticNotFound())
			return
		}
		writeAuthErr(w, err)
		return
	}

	// Audit: emit diagnostic_lookup with only the requestId in detail —
	// no raw cause, no secrets.
	acctID := sess.Account.ID
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acctID,
		Factor:    audit.FactorDiagnostic,
		Event:     audit.EventDiagnosticLookup,
		IP:        audit.ParseIPOrNil(s.clientIP.IP(r)),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"requestId": requestID},
	})

	view := diagnosticView{
		RequestID:  rec.RequestID,
		Code:       rec.Code,
		Operation:  rec.Operation,
		Method:     rec.Method,
		Route:      rec.Route,
		Retryable:  rec.Retryable,
		Fields:     rec.Fields,
		OccurredAt: rec.OccurredAt,
		ExpiresAt:  rec.ExpiresAt,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

// diagnosticRateLimitKey builds the per-account rate-limit key for diagnostic
// lookups. It formats the account ID as decimal (not a rune) so the key is
// deterministic and human-readable in logs: "diag:42", not "diag:*".
func diagnosticRateLimitKey(accountID int32) string {
	return "diag:" + strconv.FormatInt(int64(accountID), 10)
}
