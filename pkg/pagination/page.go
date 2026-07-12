// Package pagination — page.go
//
// Generic wire envelope and limit helper for paginated admin collections.
package pagination

import (
	"encoding/json"
)

// Page is the uniform wire envelope for every paginated admin collection:
//
//	{"items":[...],"nextCursor":"opaque-or-empty"}
//
// NextCursor is always present (never omitted), even on the final page where it
// serializes as the empty string. This lets clients branch on nextCursor alone
// without a separate hasMore flag.
type Page[T any] struct {
	Items      []T   `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// MarshalJSON ensures a nil Items slice serializes as [] rather than null,
// so the wire shape is always {"items":[...],"nextCursor":"..."} — clients
// never branch on items === null vs items === []. A zero-value Page emits
// {"items":[],"nextCursor":""}.
func (p Page[T]) MarshalJSON() ([]byte, error) {
	// alias avoids recursion into MarshalJSON; the inner type uses the
	// default struct marshaling.
	type alias Page[T]
	if p.Items == nil {
		p.Items = []T{}
	}
	return json.Marshal(alias(p))
}

// Limit clamps the requested page size into the allowed 1–100 range. A
// non-positive value yields the default (50); values above the maximum are
// clamped to the maximum. Limits are clamped, never rejected, so a misbehaving
// client cannot force a 400 on a list endpoint.
func Limit(v int) int {
	if v <= 0 {
		return defaultLimit
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

// jsonMarshalPage marshals a Page for the empty-final-page serialization test.
// Centralized here so the test does not re-implement encoding/json plumbing and
// stays focused on the wire shape.
func jsonMarshalPage[T any](p Page[T]) ([]byte, error) {
	return json.Marshal(p)
}
