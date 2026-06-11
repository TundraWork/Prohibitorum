// Package server — handle_auth_ratelimit.go
//
// Thin helper that applies the in-process RateLimiter at the top of each
// auth-sensitive handler. Bucket sizes are conservative — picked to be
// generous for legitimate humans but to bite any sustained spam quickly.
//
// Per-IP caps cover the unauthenticated surface (anyone can hit
// /auth/login/*, /auth/devices/pair/*, /enrollments/{token}/register/*).
// Per-account caps cover the authenticated surface where the actor's
// identity is the more useful key (multiple users behind one NAT shouldn't
// share a bucket on /me/devices/pair/approve).
package server

import (
	"net/http"
	"strconv"
	"time"

	"prohibitorum/pkg/authn"
)

// Per-IP caps on the unauthenticated auth ceremonies. These are availability /
// resource-abuse guards, NOT credential-guessing controls (the per-account
// password/TOTP throttle covers that). They sit ahead of the expensive work:
// argon2id on /auth/password/begin and WebAuthn verification + DB lookups on
// /auth/login/*. Sized generous for a NAT'd office but tight enough to bound a
// flood.
const (
	// pwdBeginIPLimit caps /auth/password/begin per IP per authIPWindow. Lower
	// than login because each request can force a full argon2id computation
	// (audit AUTHZ-1).
	pwdBeginIPLimit = 30
	// loginIPLimit caps /auth/login/begin and /auth/login/complete per IP per
	// authIPWindow (a ceremony spends one of each) (audit SESS-3).
	loginIPLimit = 60
	// authIPWindow is the fixed window for the unauthenticated auth caps.
	authIPWindow = time.Minute
	// maxPasswordBytes bounds the password accepted at /auth/password/begin
	// before it is fed to argon2id, matching /me/password/set (audit AUTHZ-1).
	maxPasswordBytes = 1024
)

// rateLimit applies a fixed-window bucket and writes the 429 response when
// the bucket is full. Returns true when the caller should ABORT (limit hit);
// false to continue.
func (s *Server) rateLimit(w http.ResponseWriter, r *http.Request, key string, max int, window time.Duration) bool {
	if s.rateLimiter.Allow(key, max, window) {
		return false
	}
	if retry := s.rateLimiter.RetryAfter(key); retry > 0 {
		// http.Header Retry-After accepts integer seconds or HTTP-date; the
		// former is friendlier to JS clients. Round up to avoid 0.
		secs := int(retry.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	writeAuthErr(w, authn.ErrRateLimited())
	return true
}
