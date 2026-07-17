package vrchat

import (
	"encoding/json"
	"fmt"
	"time"
)

type CurrentUser struct {
	ID                    string
	DisplayName           string
	RequiresTwoFactorAuth []string
}

type PublicUser struct {
	ID                             string
	DisplayName                    string
	BioLinks                       []string
	CurrentAvatarThumbnailImageURL string
}

type currentUserWire struct {
	ID                    *string         `json:"id"`
	DisplayName           *string         `json:"displayName"`
	RequiresTwoFactorAuth json.RawMessage `json:"requiresTwoFactorAuth"`
}

type publicUserWire struct {
	ID                             *string         `json:"id"`
	DisplayName                    *string         `json:"displayName"`
	BioLinks                       json.RawMessage `json:"bioLinks"`
	CurrentAvatarThumbnailImageURL *string         `json:"currentAvatarThumbnailImageUrl"`
}

type verifyResultWire struct {
	Verified *bool `json:"verified"`
}

// HTTPError reports only a bounded status classification. It never includes
// response bodies or headers.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
	Category   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("vrchat: %s (status %d)", e.Category, e.Status)
}

// IdentityMismatchError reports that VRChat returned a valid user other than
// the exact subject requested. It deliberately carries neither identifier.
type IdentityMismatchError struct{}

func (*IdentityMismatchError) Error() string { return "vrchat: returned user does not match request" }

// DecodeError reports malformed or structurally invalid upstream JSON.
type DecodeError struct{ Category string }

func (e *DecodeError) Error() string { return "vrchat: invalid " + e.Category + " response" }

// OversizeError reports that a bounded request or response exceeded its cap.
type OversizeError struct{ Category string }

func (e *OversizeError) Error() string { return "vrchat: " + e.Category + " exceeds size limit" }

// VerificationError reports a syntactically valid verification response that
// did not explicitly confirm verification.
type VerificationError struct{}

func (*VerificationError) Error() string { return "vrchat: two-factor verification failed" }

// ValidationError reports rejected caller input without echoing that input.
type ValidationError struct{ Category string }

func (e *ValidationError) Error() string { return "vrchat: invalid " + e.Category }

// RequestError deliberately omits the underlying transport error because it
// can contain URL or header-derived details.
type RequestError struct{}

func (*RequestError) Error() string { return "vrchat: request failed" }
