package contract

// Legacy pagination types (PaginatedBody, PaginationRequest, PaginatedResponse,
// PaginationInfo, EncodeCursor, DecodeCursor) have been superseded by:
//   - contract.Page[T] (the uniform wire envelope)
//   - pagination.Codec (the authenticated cursor codec)
//   - pagination.Limit (the shared limit clamp)
//
// All top-level admin indexes now use contract.Page[T] + pagination.Codec.
// This file is intentionally empty; the canonical types live in admin.go
// (Page[T]) and pkg/pagination (Codec, Limit).
