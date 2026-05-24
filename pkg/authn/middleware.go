package authn

import (
	"context"
	"time"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// SessionData is the JSON payload stored in KV under each session key. It does
// NOT snapshot role or permissions — those are re-fetched from db.Account on
// every authenticated request so admin actions (disable, role change,
// permission edits) propagate to active sessions immediately.
//
// SessionID is an opaque, non-secret handle exposed in /me/sessions so users
// can identify/revoke their other sessions without the response leaking the
// underlying cookie token. It's generated at Issue time and stable across
// refreshes.
type SessionData struct {
	SessionID  string    `json:"session_id"`
	AccountID  int32     `json:"account_id"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenIP string    `json:"last_seen_ip"`
	UserAgent  string    `json:"user_agent,omitempty"`
	// SudoUntil is the deadline for the elevated "fresh WebAuthn assertion"
	// state required by sensitive actions (currently /me/devices/pair/approve).
	// Zero or past means no sudo; future means the gate is satisfied. Set by
	// POST /me/sudo/complete and consumed inline by the gated handlers.
	SudoUntil time.Time `json:"sudo_until,omitempty"`
}

// HasFreshSudo returns true when the session's SudoUntil is in the future.
func (d *SessionData) HasFreshSudo() bool {
	return !d.SudoUntil.IsZero() && time.Now().Before(d.SudoUntil)
}

// Session is the per-request authentication record placed on the context by
// session.LoadSession. Token + Data are the cookie + KV view; Account is the
// freshly loaded db row (live — never snapshotted into the session blob).
//
// The type lives in pkg/authn so that pkg/session can import pkg/authn
// without creating a circular dependency (authn.Check takes *authn.Session).
type Session struct {
	Account *db.Account
	Token   string
	Data    *SessionData
}

type ctxKey struct{ name string }

var sessionCtxKey = ctxKey{name: "session"}

// WithSession returns a new context with the session attached.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}

// SessionFromContext returns the session attached by session.LoadSession, or
// nil if the request is unauthenticated.
func SessionFromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(sessionCtxKey).(*Session)
	return s
}

// Check enforces the AuthRequirement against the (possibly nil) session.
// Returns nil on success or the canonical AuthError on failure.
func Check(s *Session, req contract.AuthRequirement) error {
	if req.Kind == contract.AuthPublic {
		return nil
	}
	if s == nil {
		return ErrNoSession()
	}
	// Disabled-session sentinel from LoadSession — reject every non-public
	// route with the JSON-envelope account_disabled code so the dashboard's
	// ApiRequestError carries a machine-readable code.
	if s.Account.Disabled {
		return ErrAccountDisabled()
	}
	switch req.Kind {
	case contract.AuthSession:
		return nil
	case contract.AuthAdmin:
		if s.Account.Role != "admin" {
			return ErrNotAdmin()
		}
		return nil
	}
	return ErrNoSession()
}
