// Package contract — admin.go
//
// Admin-cursor pagination contract: the uniform Page[T] envelope shared with
// pkg/pagination, the registered pagination_cursor_invalid public-error code,
// and the typed CursorInvalidError handlers return when a cursor fails
// validation (tamper, expiry, wrong endpoint/filter/sort binding).
//
// The codec itself lives in pkg/pagination; this file owns the wire-facing
// types and the registered error code so handlers depend on a single contract
// surface for both request and response shapes.
package contract

import (
	"net/http"

	"prohibitorum/pkg/weberr"
)

// Page is the admin wire envelope for a paginated collection. It mirrors
// pagination.Page[T] but lives in contract so handlers and OpenAPI docs share a
// single canonical type without importing the codec package. NextCursor is
// always present, serializing as "" on the final page.
//
// NOTE: the generic contract.Page exists for typed Huma I/O. Raw chi handlers
// that return pagination.Page[T] produce the identical JSON shape.
type Page[T any] struct {
	Items      []T   `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// CodeCursorInvalid is the registered public-error code returned when a
// pagination cursor is invalid: malformed, tampered, expired, or bound to a
// different endpoint / filter set / sort order. Handlers MUST map
// pagination.ErrCursorInvalid (and any decode failure) to this code so clients
// see a stable, documented reason rather than a generic bad_request.
const CodeCursorInvalid = "pagination_cursor_invalid"

func init() {
	if err := weberr.Register([]weberr.Definition{
		{
			Code:           CodeCursorInvalid,
			Status:         http.StatusBadRequest,
			LocaleKey:      "errors.pagination_cursor_invalid",
			Retryable:      false,
			Recovery:       "restart_pagination",
			DiagnosticKind: "validation",
		},
	}); err != nil {
		panic("contract: failed to register pagination_cursor_invalid: " + err.Error())
	}
}

// CursorInvalidError returns a *weberr.PublicError for an invalid cursor.
// Handlers call this when pagination.ErrCursorInvalid (or a codec Decode
// failure) is returned; the wire envelope is {code, requestId} with HTTP 400.
func CursorInvalidError() error {
	return weberr.New(CodeCursorInvalid, nil)
}
