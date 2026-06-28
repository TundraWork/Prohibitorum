// Package pat implements user-owned Personal Access Tokens: a high-entropy
// bearer credential a user presents at the forward-auth gateway. Tokens are
// validated on the request hot path, so they are hashed with SHA-256 (fast,
// safe for 256-bit random secrets) — NOT argon2id, which is for low-entropy
// passwords verified off the hot path.
package pat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// Prefix is the fixed, recognizable token prefix. It enables secret-scanning
// tools to flag a leaked PAT.
const Prefix = "prohibitorum_pat_"

// Generate returns a new PAT: the plaintext (shown to the user exactly once),
// its SHA-256 hash (stored), and a non-secret hint for the list UI.
func Generate() (raw string, hash []byte, hint string, err error) {
	var buf [32]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return "", nil, "", err
	}
	raw = Prefix + base64.RawURLEncoding.EncodeToString(buf[:])
	return raw, HashToken(raw), Hint(raw), nil
}

// HashToken returns the SHA-256 hash of a raw token for storage and lookup.
func HashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

// Hint returns a non-secret display string: the prefix + an ellipsis + the last
// 4 chars of the token. Too little entropy to aid a brute force.
func Hint(raw string) string {
	last := raw
	if len(raw) > 4 {
		last = raw[len(raw)-4:]
	}
	return Prefix + "…" + last
}
