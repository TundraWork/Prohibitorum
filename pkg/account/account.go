package account

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"prohibitorum/pkg/authn"
)

// usernameRegex matches the strict username rule from
// specs/2026-05-23-auth-system/design.md §1: lowercase ASCII letters,
// digits, underscore, dash; 2-32 chars. No normalization performed —
// rejects strict per CLAUDE.md "fail fast on input".
var usernameRegex = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

// ValidateUsername returns nil iff s matches the username regex.
// Returns *authn.AuthError with code "invalid_username" on failure.
func ValidateUsername(s string) error {
	if !usernameRegex.MatchString(s) {
		return authn.ErrInvalidUsername()
	}
	return nil
}

// ValidateDisplayName returns nil iff s is 1-128 characters long and
// contains no control characters (0x00-0x1f, 0x7f). No trimming or
// normalization.
func ValidateDisplayName(s string) error {
	if len(s) < 1 || len(s) > 128 {
		return authn.ErrInvalidDisplayName()
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return authn.ErrInvalidDisplayName()
		}
	}
	return nil
}

// ValidateNickname permits nil/empty (clears the nickname) OR a 1-60 char
// non-control-character string. Whitespace-only strings are treated as empty.
func ValidateNickname(s *string) error {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	if len(v) > 60 {
		return authn.ErrInvalidNickname()
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return authn.ErrInvalidNickname()
		}
	}
	return nil
}

// NormalizeNickname returns the trimmed value, or nil if the result would be
// empty (so the DB column ends up NULL rather than empty-string).
func NormalizeNickname(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// GenerateUserHandle returns 64 cryptographically-random bytes intended
// for use as the WebAuthn user.id of a new account. Persisted in
// account.webauthn_user_handle.
func GenerateUserHandle() ([]byte, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate user handle: %w", err)
	}
	return b, nil
}

