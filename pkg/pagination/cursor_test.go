// Package pagination — cursor_test.go
//
// Tests the stateless, DEK-protected admin pagination cursor: AES-GCM
// authenticated payloads bound to collection / filters / sort / keyset, with a
// 24h expiry and DEK-version rotation support. A cursor reused against another
// endpoint, filter set, sort order, or after tampering/expiry is rejected with
// ErrCursorInvalid.
package pagination

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// testDEK returns a deterministic 32-byte AES-256 key for the given version.
func testDEK(version int) []byte {
	return bytes.Repeat([]byte{byte(0x10 + version)}, 32)
}

// randDEK returns a random 32-byte AES-256 key (for tamper tests so the
// "modified ciphertext" differs from a fresh encryption with high probability).
func randDEK(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func mustEncode(t *testing.T, kc *Codec, p CursorPayload) string {
	t.Helper()
	s, err := kc.Encode(p)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return s
}

func mustDecode(t *testing.T, kc *Codec, s, collection, sort string, filters map[string]string) (CursorPayload, error) {
	t.Helper()
	p, err := kc.Decode(s, collection, sort, filters)
	if err != nil {
		return CursorPayload{}, err
	}
	return p, nil
}

// --- Round trip ---

func TestCursorRoundTrip(t *testing.T) {
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	in := CursorPayload{
		Collection: "accounts",
		Filters:    map[string]string{"q": "alice", "role": "admin"},
		Sort:       "created_at",
		Keys:       []string{"2026-07-13T10:00:00Z", "42"},
	}
	s := mustEncode(t, kc, in)
	if s == "" {
		t.Fatal("encoded cursor is empty")
	}
	out, err := mustDecode(t, kc, s, "accounts", "created_at", map[string]string{"q": "alice", "role": "admin"})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Collection != "accounts" {
		t.Errorf("Collection = %q, want %q", out.Collection, "accounts")
	}
	if out.Sort != "created_at" {
		t.Errorf("Sort = %q, want %q", out.Sort, "created_at")
	}
	if len(out.Keys) != 2 || out.Keys[0] != "2026-07-13T10:00:00Z" || out.Keys[1] != "42" {
		t.Errorf("Keys = %v, want [2026-07-13T10:00:00Z, 42]", out.Keys)
	}
	if out.Filters["q"] != "alice" || out.Filters["role"] != "admin" {
		t.Errorf("Filters = %v, want q=alice role=admin", out.Filters)
	}
}

// --- Tamper detection: modified ciphertext ---

func TestCursorTamperDetectionModifiedCiphertext(t *testing.T) {
	dek := randDEK(t)
	kc := NewCodec(map[int][]byte{1: dek}, 1, time.Now)
	s := mustEncode(t, kc, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if len(raw) < 4 {
		t.Fatalf("cursor too short: %d bytes", len(raw))
	}
	// Flip a byte in the ciphertext region (after version prefix + nonce).
	// GCM tag covers everything, so any flip must fail authentication.
	idx := len(raw) - 4
	raw[idx] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	if _, err := kc.Decode(tampered, "accounts", "created_at", nil); err == nil {
		t.Fatal("Decode accepted a tampered ciphertext; want ErrCursorInvalid")
	} else if !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("Decode tampered: err = %v, want ErrCursorInvalid", err)
	}
}

// --- Wrong endpoint (collection) rejection ---

func TestCursorWrongEndpointRejection(t *testing.T) {
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	s := mustEncode(t, kc, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})
	if _, err := kc.Decode(s, "groups", "created_at", nil); err == nil {
		t.Fatal("Decode accepted cursor bound to a different collection")
	} else if !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong collection: err = %v, want ErrCursorInvalid", err)
	}
}

// --- Wrong filter rejection ---

func TestCursorWrongFilterRejection(t *testing.T) {
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	s := mustEncode(t, kc, CursorPayload{
		Collection: "accounts",
		Filters:    map[string]string{"q": "alice"},
		Sort:       "created_at",
		Keys:       []string{"1"},
	})
	cases := []map[string]string{
		{"q": "bob"},            // value differs
		{"q": "alice", "x": "y"}, // extra key
		{},                       // missing key
		{"role": "admin"},        // different key
	}
	for _, filters := range cases {
		if _, err := kc.Decode(s, "accounts", "created_at", filters); err == nil {
			t.Fatalf("Decode accepted cursor with mismatched filters %v", filters)
		} else if !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("wrong filters %v: err = %v, want ErrCursorInvalid", filters, err)
		}
	}
}

// --- Wrong sort rejection ---

func TestCursorWrongSortRejection(t *testing.T) {
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	s := mustEncode(t, kc, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})
	if _, err := kc.Decode(s, "accounts", "name", nil); err == nil {
		t.Fatal("Decode accepted cursor bound to a different sort")
	} else if !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong sort: err = %v, want ErrCursorInvalid", err)
	}
}

// --- Expiry (24h) ---

func TestCursorExpiry(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, func() time.Time { return now })
	s := mustEncode(t, kc, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})

	// Within the 24h window: decodes.
	if _, err := kc.Decode(s, "accounts", "created_at", nil); err != nil {
		t.Fatalf("within 24h: Decode: %v", err)
	}

	// Advance just past 24h: rejected as expired.
	expired := NewCodec(map[int][]byte{1: testDEK(1)}, 1, func() time.Time {
		return now.Add(cursorTTL + time.Second)
	})
	if _, err := expired.Decode(s, "accounts", "created_at", nil); err == nil {
		t.Fatal("Decode accepted an expired cursor")
	} else if !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("expired: err = %v, want ErrCursorInvalid", err)
	}
}

// --- DEK version rotation: old key still decodes ---

func TestCursorDEKRotationOldKeyStillDecodes(t *testing.T) {
	// Encode with v1 active.
	v1 := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	s := mustEncode(t, v1, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})

	// Rotate: v2 is now active, v1 retained. The v1-issued cursor must still
	// decode because the codec addresses the key by the version prefix.
	rotated := NewCodec(map[int][]byte{1: testDEK(1), 2: testDEK(2)}, 2, time.Now)
	out, err := rotated.Decode(s, "accounts", "created_at", nil)
	if err != nil {
		t.Fatalf("rotated codec: Decode: %v", err)
	}
	if out.Collection != "accounts" {
		t.Errorf("Collection = %q, want accounts", out.Collection)
	}
}

// Encode after rotation stamps the new active version, so a fresh cursor seals
// under v2 and decodes with the rotated codec.
func TestCursorEncodeUsesActiveVersion(t *testing.T) {
	rotated := NewCodec(map[int][]byte{1: testDEK(1), 2: testDEK(2)}, 2, time.Now)
	s := mustEncode(t, rotated, CursorPayload{Collection: "accounts", Sort: "created_at", Keys: []string{"1"}})

	// A codec that only retains v1 cannot decode a v2 cursor.
	v1Only := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	if _, err := v1Only.Decode(s, "accounts", "created_at", nil); err == nil {
		t.Fatal("v1-only codec decoded a v2 cursor; want ErrCursorInvalid")
	} else if !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("v1-only on v2 cursor: err = %v, want ErrCursorInvalid", err)
	}
}

// --- Malformed cursor ---

func TestCursorMalformed(t *testing.T) {
	kc := NewCodec(map[int][]byte{1: testDEK(1)}, 1, time.Now)
	cases := []string{
		"",                       // empty
		"!!!not-base64!!!",       // invalid base64
		"AAAA",                   // too short for version prefix
		"AAAAAAAAAAAAAAAA",       // too short for nonce
		"_-_-_notreal",           // base64-ish but truncated
	}
	for _, c := range cases {
		if _, err := kc.Decode(c, "accounts", "created_at", nil); err == nil {
			t.Fatalf("Decode accepted malformed cursor %q", c)
		} else if !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("malformed %q: err = %v, want ErrCursorInvalid", c, err)
		}
	}
}

// --- Empty nextCursor final page ---

func TestPageEmptyNextCursorFinalPage(t *testing.T) {
	p := Page[int]{Items: []int{}, NextCursor: ""}
	b, err := jsonMarshalPage(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"items":[],"nextCursor":""}`
	if string(b) != want {
		t.Errorf("empty page JSON = %s, want %s", string(b), want)
	}
}

// A non-empty page with no next cursor still serializes nextCursor as "".
func TestPageNonEmptyWithEmptyNextCursor(t *testing.T) {
	p := Page[int]{Items: []int{1, 2, 3}, NextCursor: ""}
	b, err := jsonMarshalPage(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"items":[1,2,3],"nextCursor":""}`
	if string(b) != want {
		t.Errorf("non-empty page JSON = %s, want %s", string(b), want)
	}
}

// A zero-value Page (nil Items) must serialize items as [] not null, so
// clients never have to branch on items === null vs items === [].
func TestPageNilItemsSerializesAsEmptyArray(t *testing.T) {
	b, err := jsonMarshalPage(Page[int]{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"items":[],"nextCursor":""}`
	if string(b) != want {
		t.Errorf("nil-items page JSON = %s, want %s", string(b), want)
	}
}

// --- Limit clamp ---

func TestClampLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 50},      // default
		{-1, 50},     // negative → default
		{-100, 50},   // large negative → default
		{1, 1},       // minimum in-range
		{50, 50},     // exactly default
		{100, 100},   // maximum in-range
		{101, 100},   // above max → clamp
		{1000, 100},  // way above max → clamp
		{75, 75},     // mid-range unchanged
	}
	for _, c := range cases {
		if got := Limit(c.in); got != c.want {
			t.Errorf("Limit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
