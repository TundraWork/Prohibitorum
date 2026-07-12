// Package pagination implements the stateless, DEK-protected admin cursor
// codec and the uniform Page[T] wire envelope used by every paginated admin
// collection.
//
// Cursor design. A cursor is an authenticated AES-256-GCM payload that binds
// together the collection (endpoint), normalized filters, sort identifier, the
// keyset position, the issue time, and a 24-hour expiry. The active DEK seals
// the payload; a single leading version byte addresses the retained key at
// decode time so cursors issued before a DEK rotation keep decoding until they
// expire. The GCM additional authenticated data covers the version prefix so a
// version byte lifted from another cursor fails authentication.
//
// Binding. Decode validates the collection, filters, and sort against the
// caller-supplied values BEFORE exposing the keyset. Any mismatch, tampering,
// expiry, malformed payload, or missing DEK version surfaces as
// ErrCursorInvalid — handlers map this to the registered
// pagination_cursor_invalid public-error code.
//
// Page. The generic Page[T] envelope serializes as
//
//	{"items":[...],"nextCursor":"opaque-or-empty"}
//
// An empty / final page emits items:[] and nextCursor:"" (never omitted) so
// clients can branch on nextCursor alone without a separate hasMore flag.
package pagination

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// cursorTTL is the maximum lifetime of an issued cursor. A cursor older than
// this is rejected at decode time as expired (mapped to ErrCursorInvalid →
// pagination_cursor_invalid).
const cursorTTL = 24 * time.Hour

// defaultLimit is the limit applied when a caller omits the limit or passes a
// non-positive value. Limits below 1 or above maxLimit are clamped, never
// rejected, so a misbehaving client can't force a 400 on a list endpoint.
const (
	defaultLimit = 50
	maxLimit     = 100
)

// versionLen is the fixed length of the version prefix (one byte). Keeping it
// single-byte is sufficient for DEK versions 1–255 and keeps the cursor short.
const versionLen = 1

// ErrCursorInvalid is the single sentinel returned for every cursor failure:
// tampering, expiry, malformed input, wrong collection/filter/sort binding, or
// a missing DEK version. Handlers MUST map this to the registered
// pagination_cursor_invalid public-error code; the underlying cause (GCM auth
// failure, JSON error, version mismatch) is intentionally not distinguished on
// the wire so a client cannot probe WHY a cursor was rejected.
var ErrCursorInvalid = errors.New("pagination: cursor invalid")

// CursorPayload is the authenticated state carried inside an opaque cursor.
// The fields bind the cursor to a single (collection, filters, sort) context
// plus the keyset position and a bounded lifetime.
type CursorPayload struct {
	// Collection identifies the endpoint the cursor was issued for (e.g.
	// "accounts", "groups"). A cursor decoded against a different collection
	// is rejected.
	Collection string `json:"collection"`
	// Filters is the normalized filter set active when the cursor was issued.
	// Both keys and values must match at decode time; an extra, missing, or
	// value-differing filter rejects the cursor.
	Filters map[string]string `json:"filters,omitempty"`
	// Sort is the sort identifier the cursor is bound to (e.g. "created_at",
	// "name"). A different sort order rejects the cursor.
	Sort string `json:"sort"`
	// Keys is the keyset position — the tuple of the last row returned, in the
	// query's native ordering. The caller projects this back into the keyset
	// WHERE clause. Exposed only after binding validation passes.
	Keys []string `json:"keys"`
	// IssuedAt is the encode time, used together with cursorTTL to bound the
	// cursor's lifetime.
	IssuedAt time.Time `json:"iat"`
	// ExpiresAt is IssuedAt + cursorTTL. Stored explicitly (rather than derived
	// at decode) so the lifetime is self-describing and survives a clock skew
	// between encode and decode up to the expiry check itself.
	ExpiresAt time.Time `json:"exp"`
}

// Codec seals and opens CursorPayload values with the configured DEK set. New
// cursors are sealed with the active DEK; decode addresses the retained key by
// the version prefix, so a cursor issued before a rotation keeps decoding until
// it expires. The now hook is injected so tests can drive the clock for the
// expiry path.
type Codec struct {
	deks          map[int][]byte
	activeVersion int
	now           func() time.Time
}

// NewCodec returns a Codec over the given versioned DEK map. activeVersion is
// the version new cursors are sealed with; it MUST be present in deks. now is
// used for the expiry check; pass time.Now for production.
func NewCodec(deks map[int][]byte, activeVersion int, now func() time.Time) *Codec {
	return &Codec{deks: deks, activeVersion: activeVersion, now: now}
}

// Encode seals the payload under the active DEK and returns the opaque cursor
// string: base64url(version || nonce || ciphertext). The version prefix lets a
// rotated codec address the retained key at decode time; the GCM AAD covers
// the version byte so it cannot be swapped.
func (c *Codec) Encode(p CursorPayload) (string, error) {
	dek, ok := c.deks[c.activeVersion]
	if !ok {
		return "", fmt.Errorf("pagination: active DEK version %d not configured", c.activeVersion)
	}
	if len(dek) != 32 {
		return "", fmt.Errorf("pagination: DEK v%d must be 32 bytes (AES-256), got %d", c.activeVersion, len(dek))
	}
	now := c.now()
	p.IssuedAt = now
	p.ExpiresAt = now.Add(cursorTTL)
	plaintext, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("pagination: marshal cursor payload: %w", err)
	}
	aead, err := newGCM(dek)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("pagination: read nonce: %w", err)
	}
	version := byte(c.activeVersion)
	aad := []byte{version}
	ct := aead.Seal(nil, nonce, plaintext, aad)
	// Layout: version || nonce || ciphertext. Decoding slices the prefix by
	// fixed lengths, so changes here MUST stay in sync with Decode.
	buf := make([]byte, 0, versionLen+len(nonce)+len(ct))
	buf = append(buf, version)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Decode opens the opaque cursor, validates collection/filters/sort bindings,
// checks expiry, and returns the keyset payload. Any failure — malformed input,
// tampering, wrong key, missing DEK version, binding mismatch, or expiry —
// returns ErrCursorInvalid (wrapped cause is not exposed on the wire).
//
// filters is the caller's normalized filter map; it is compared against the
// payload's Filters for exact key+value equality. An empty filters map on both
// sides is valid. Sort is compared as a plain string.
func (c *Codec) Decode(cursor, collection, sort string, filters map[string]string) (CursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return CursorPayload{}, ErrCursorInvalid
	}
	if len(raw) < versionLen {
		return CursorPayload{}, ErrCursorInvalid
	}
	version := int(raw[0])
	dek, ok := c.deks[version]
	if !ok {
		// Missing DEK version (rotated away): the cursor can no longer be
		// opened. Surface as invalid, not as an internal error.
		return CursorPayload{}, ErrCursorInvalid
	}
	if len(dek) != 32 {
		return CursorPayload{}, ErrCursorInvalid
	}
	aead, err := newGCM(dek)
	if err != nil {
		return CursorPayload{}, ErrCursorInvalid
	}
	ns := aead.NonceSize()
	// Need at least version + nonce + one tag byte; a too-short input fails
	// GCM.Open below anyway, but gate explicitly so the failure mode is the
	// sentinel rather than a slice-bounds panic.
	if len(raw) < versionLen+ns+1 {
		return CursorPayload{}, ErrCursorInvalid
	}
	nonce := raw[versionLen : versionLen+ns]
	ct := raw[versionLen+ns:]
	aad := raw[:versionLen] // version prefix is authenticated
	plaintext, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return CursorPayload{}, ErrCursorInvalid
	}
	var p CursorPayload
	if err := json.Unmarshal(plaintext, &p); err != nil {
		return CursorPayload{}, ErrCursorInvalid
	}
	// Binding: collection, filters, sort must match the caller's request.
	if p.Collection != collection {
		return CursorPayload{}, ErrCursorInvalid
	}
	if p.Sort != sort {
		return CursorPayload{}, ErrCursorInvalid
	}
	if !filtersEqual(p.Filters, filters) {
		return CursorPayload{}, ErrCursorInvalid
	}
	// Expiry: reject if the current time is at or past ExpiresAt.
	if !c.now().Before(p.ExpiresAt) {
		return CursorPayload{}, ErrCursorInvalid
	}
	return p, nil
}

// filtersEqual reports whether two filter maps have identical key→value pairs.
// nil and empty maps are considered equal. Keys are compared without sorting by
// direct map iteration, which is safe because a differing key set yields a
// mismatch on the first extra/missing key.
func filtersEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av != bv {
			return false
		}
	}
	return true
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key. Returns an error (not
// the sentinel) on the crypto/init path; callers gate the length check first.
func newGCM(dek []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("pagination: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("pagination: cipher.NewGCM: %w", err)
	}
	return aead, nil
}
