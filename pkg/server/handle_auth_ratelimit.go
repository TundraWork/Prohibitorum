// Package server — handle_auth_ratelimit.go
//
// Thin helper that applies the in-process RateLimiter at the top of each
// auth-sensitive handler. Bucket sizes are conservative — picked to be
// generous for legitimate humans but to bite any sustained spam quickly.
//
// Buckets key on the account or session, never on the client IP: behind a
// reverse proxy or NAT every user shares one address, so a per-IP bucket
// would let one client exhaust everyone's budget (PHB-181). The
// unauthenticated sign-in routes have no bucket; the argon2id slot limit in
// pkg/credential/password bounds the cost of a flood there, and volumetric
// protection belongs to the reverse proxy or WAF.
package server

import (
	"net/http"
	"strconv"
	"time"

	"prohibitorum/pkg/authn"
)

const (
	// maxPasswordBytes bounds the password accepted at /auth/password/begin
	// before it is fed to argon2id, matching /me/password/set (audit AUTHZ-1).
	maxPasswordBytes = 1024
	// maxPasswordBeginBodyBytes bounds the /auth/password/begin body before it
	// is decoded. A username, a maxPasswordBytes password and the remember flag
	// fit well inside it.
	maxPasswordBeginBodyBytes = 4 << 10
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
