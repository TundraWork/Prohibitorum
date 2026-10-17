package enrollment

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// Enrollment intent constants — match the CHECK constraint on the enrollment table.
const (
	IntentBootstrap = "bootstrap"
	IntentInvite    = "invite"
	IntentReset     = "reset"
	IntentAddDevice = "add_device"
)

// DefaultEnrollmentTTL is the lifetime of an issued enrollment URL.
const DefaultEnrollmentTTL = 24 * time.Hour

// newEnrollmentToken returns a URL-safe base64 encoding of 32 random bytes (43 chars).
func newEnrollmentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("enrollment: rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// EnrollmentTemplate carries the role + attributes for an invite-intent
// enrollment. The account is created at consume time using the invitee's
// chosen username + displayName plus this template's role + attributes.
// Only meaningful for IntentInvite; pass nil for bootstrap and reset.
type EnrollmentTemplate struct {
	Role                    string         // "admin" or "user"
	Attributes              map[string]any // arbitrary claim attributes; stored as JSONB
	ExpectedUpstreamIDPSlug *string        // optional; pre-binds invite to a specific upstream IdP
	Username                *string        // optional fixed local username
	GroupIDs                []int32        // manual groups applied when the invite is redeemed
	CreatedByAccountID      *int32         // invitation creator, recorded on manual group decisions
	// RequireLocalCredential marks an invite whose redemption must set a
	// local sign-in method before the account is created.
	RequireLocalCredential bool
}

// InvitationGroupApplier is the transaction-scoped storage operation used
// while redeeming an invitation. Callers must pass the queries bound to the
// same transaction that consumes the invitation and creates the account.
type InvitationGroupApplier interface {
	ApplyInvitationGroups(context.Context, db.ApplyInvitationGroupsParams) ([]int32, error)
}

// NormalizeGroupIDs removes duplicate group IDs while preserving their first
// occurrence. Invitation creation and redemption share this normalization so
// an invitation cannot produce duplicate manual decisions.
func NormalizeGroupIDs(groupIDs []int32) []int32 {
	if len(groupIDs) == 0 {
		return []int32{}
	}
	normalized := make([]int32, 0, len(groupIDs))
	seen := make(map[int32]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		normalized = append(normalized, groupID)
	}
	return normalized
}

// ApplyInvitationGroups verifies that every saved group still exists as a
// manual group, then writes allow decisions for the newly created account. A
// missing or non-manual group returns a public conflict error. The surrounding
// transaction must roll back that error so the invitation remains reusable.
func ApplyInvitationGroups(
	ctx context.Context,
	q InvitationGroupApplier,
	groupIDs []int32,
	accountID int32,
	createdByAccountID pgtype.Int4,
) error {
	requested := NormalizeGroupIDs(groupIDs)
	if len(requested) == 0 {
		return nil
	}
	applied, err := q.ApplyInvitationGroups(ctx, db.ApplyInvitationGroupsParams{
		GroupIds:  requested,
		AccountID: accountID,
		CreatedBy: createdByAccountID,
	})
	if err != nil {
		return fmt.Errorf("enrollment: apply invitation groups: %w", err)
	}
	appliedSet := make(map[int32]struct{}, len(applied))
	for _, groupID := range applied {
		appliedSet[groupID] = struct{}{}
	}
	invalid := make([]int32, 0, len(requested))
	for _, groupID := range requested {
		if _, ok := appliedSet[groupID]; !ok {
			invalid = append(invalid, groupID)
		}
	}
	if len(invalid) != 0 {
		return authn.ErrInvitationGroupsUnavailable(invalid)
	}
	return nil
}

// IssueEnrollment inserts a new enrollment row and returns the token + expiry.
// For intent='bootstrap', targetAccountID must be nil; for 'reset' it must
// reference an existing account.id; for 'invite' it MUST be nil (per the
// CHECK constraint). The CHECK constraint enforces this server-side regardless.
//
// ttl <= 0 falls back to DefaultEnrollmentTTL.
//
// tpl is required for IntentInvite and forbidden for the other intents (the
// template_intent_check constraint will reject the insert otherwise). Pass
// nil for bootstrap and reset.
func IssueEnrollment(
	ctx context.Context,
	q db.Querier,
	intent string,
	targetAccountID *int32,
	ttl time.Duration,
	tpl *EnrollmentTemplate,
) (string, time.Time, error) {
	if ttl == 0 {
		ttl = DefaultEnrollmentTTL
	}
	token, err := newEnrollmentToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(ttl)
	var tgt pgtype.Int4
	if targetAccountID != nil {
		tgt = pgtype.Int4{Int32: *targetAccountID, Valid: true}
	}
	params := db.InsertEnrollmentParams{
		Token:           token,
		Intent:          intent,
		TargetAccountID: tgt,
		ExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true},
		GroupIds:        []int32{},
	}
	if tpl != nil {
		params.TemplateRole = pgtype.Text{String: tpl.Role, Valid: tpl.Role != ""}
		if tpl.Attributes != nil {
			raw, err := json.Marshal(tpl.Attributes)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("enrollment: marshal attributes: %w", err)
			}
			params.TemplateAttributes = raw
		}
		// An empty slug is "no provider required", the same reading the caller
		// applies before it validates the slug exists: storing it as a binding
		// would persist a provider requirement that was never checked and that
		// no provider can satisfy.
		if tpl.ExpectedUpstreamIDPSlug != nil && *tpl.ExpectedUpstreamIDPSlug != "" {
			params.ExpectedUpstreamIdpSlug = pgtype.Text{String: *tpl.ExpectedUpstreamIDPSlug, Valid: true}
		}
		if tpl.Username != nil && *tpl.Username != "" {
			params.TemplateUsername = pgtype.Text{String: *tpl.Username, Valid: true}
		}
		params.GroupIds = NormalizeGroupIDs(tpl.GroupIDs)
		if tpl.CreatedByAccountID != nil {
			params.CreatedByAccountID = pgtype.Int4{Int32: *tpl.CreatedByAccountID, Valid: true}
		}
		params.RequireLocalCredential = tpl.RequireLocalCredential
	}
	if _, err := q.InsertEnrollment(ctx, params); err != nil {
		return "", time.Time{}, fmt.Errorf("enrollment: insert: %w", err)
	}
	return token, expiresAt, nil
}

// LoadEnrollment fetches and validates an enrollment by its plaintext token.
// Returns:
//   - ErrEnrollmentConsumed: row missing (never issued OR cascade-deleted by
//     target account removal) OR consumed_at is set.
//   - ErrEnrollmentExpired: row exists but past expires_at.
//   - other error: DB failure.
//
// We collapse "never existed" and "consumed" into the same UX-facing error
// because the URL holder doesn't need to distinguish, and the underlying state
// (target account was deleted) shouldn't leak.
func LoadEnrollment(ctx context.Context, q db.Querier, token string) (*db.Enrollment, error) {
	row, err := q.GetEnrollmentByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authn.ErrEnrollmentConsumed()
		}
		return nil, fmt.Errorf("enrollment: get: %w", err)
	}
	if row.ConsumedAt.Valid {
		return nil, authn.ErrEnrollmentConsumed()
	}
	if !row.ExpiresAt.Valid {
		// Shouldn't happen (NOT NULL column), but defensively treat as expired.
		return nil, authn.ErrEnrollmentExpired()
	}
	if time.Now().After(row.ExpiresAt.Time) {
		return nil, authn.ErrEnrollmentExpired()
	}
	return &row, nil
}

// ConsumeEnrollment atomically marks a token consumed and returns the row.
// Returns ErrEnrollmentConsumed if the token was already consumed, missing,
// or expired — the SQL WHERE clause gates on both consumed_at IS NULL and
// expires_at > now(), so pgx.ErrNoRows covers all "not consumable" cases.
//
// Must be called inside the same TX as the credential / account insert so a
// crash between operations doesn't allow the same token to be reused, AND so
// concurrent requests serialize on the row-level lock from the conditional
// UPDATE.
func ConsumeEnrollment(ctx context.Context, q db.Querier, token string) (*db.Enrollment, error) {
	row, err := q.ConsumeEnrollment(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authn.ErrEnrollmentConsumed()
		}
		return nil, fmt.Errorf("enrollment: consume: %w", err)
	}
	return &row, nil
}

// DecodeTemplateAttributes decodes the jsonb template_attributes column into a
// map. Returns nil if the bytes are empty or null.
func DecodeTemplateAttributes(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
