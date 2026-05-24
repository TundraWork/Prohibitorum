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

	"prohibitorum/pkg/auth"
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
	writeAuthErr(w, auth.ErrRateLimited())
	return true
}
