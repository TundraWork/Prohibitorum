package contract

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type PaginationRequest struct {
	Limit  int32  `query:"limit" example:"20" default:"20" maximum:"100" minimum:"1"`
	Cursor string `query:"cursor" example:"eyJpZCI6MX0="`
}

type PaginationInfo struct {
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
}

type PaginatedBody[T any] struct {
	Items      []T            `json:"items"`
	Pagination PaginationInfo `json:"pagination"`
}

type PaginatedResponse[T any] struct {
	Body PaginatedBody[T]
}

func EncodeCursor(values ...any) (string, error) {
	m := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return "", fmt.Errorf("cursor key at index %d must be string", i)
		}
		m[key] = values[i+1]
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func DecodeCursor(cursor string, targets ...any) error {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return fmt.Errorf("decode cursor: %w", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("unmarshal cursor: %w", err)
	}
	for i := 0; i < len(targets); i += 2 {
		key, ok := targets[i].(string)
		if !ok {
			return fmt.Errorf("cursor key at index %d must be string", i)
		}
		raw, exists := m[key]
		if !exists {
			continue
		}
		if err := json.Unmarshal(raw, targets[i+1]); err != nil {
			return fmt.Errorf("unmarshal cursor field %q: %w", key, err)
		}
	}
	return nil
}
