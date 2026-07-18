package enrollment

import (
	"bytes"
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
	IntentBootstrap         = "bootstrap"
	IntentInvite            = "invite"
	IntentReset             = "reset"
	IntentAddDevice         = "add_device"
	IntentFederatedRegister = "federated_register"
)

// DefaultEnrollmentTTL is the lifetime of an issued enrollment URL.
const DefaultEnrollmentTTL = 24 * time.Hour

// FederatedEnrollmentTTL is the default lifetime for provider-verified
// registration and recovery enrollments.
const FederatedEnrollmentTTL = 15 * time.Minute

// FederatedIdentitySnapshot contains only adapter-verified identity data.
type FederatedIdentitySnapshot struct {
	UpstreamIDPID   int64
	UpstreamIDPSlug string
	Issuer          string
	Subject         string
	DisplayName     string
	UpstreamData    []byte
	AvatarURL       *string
}

// FederatedRegistrationInserter is the storage operation required to issue a
// federated-registration enrollment.
type FederatedRegistrationInserter interface {
	InsertFederatedRegistrationEnrollment(context.Context, db.InsertFederatedRegistrationEnrollmentParams) (db.Enrollment, error)
}

// ProviderRecoveryInserter is the storage operation required to issue a
// provider-recovery enrollment.
type ProviderRecoveryInserter interface {
	InsertProviderRecoveryEnrollment(context.Context, db.InsertProviderRecoveryEnrollmentParams) (db.Enrollment, error)
}

func validateFederatedIdentitySnapshot(snapshot FederatedIdentitySnapshot) error {
	switch {
	case snapshot.UpstreamIDPID <= 0:
		return errors.New("enrollment: federated snapshot missing provider ID")
	case snapshot.UpstreamIDPSlug == "":
		return errors.New("enrollment: federated snapshot missing provider slug")
	case snapshot.Issuer == "":
		return errors.New("enrollment: federated snapshot missing issuer")
	case snapshot.Subject == "":
		return errors.New("enrollment: federated snapshot missing subject")
	case snapshot.DisplayName == "":
		return errors.New("enrollment: federated snapshot missing display name")
	case len(snapshot.UpstreamData) > 4096:
		return errors.New("enrollment: federated snapshot metadata is too large")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.UpstreamData, &object); err != nil || object == nil {
		return errors.New("enrollment: federated snapshot metadata must be a JSON object")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("enrollment: canonicalize federated snapshot metadata: %w", err)
	}
	if !bytes.Equal(snapshot.UpstreamData, canonical) {
		return errors.New("enrollment: federated snapshot metadata is not canonical JSON")
	}
	return nil
}

// IssueFederatedRegistration stores a short-lived, server-side identity
// snapshot for account creation.
func IssueFederatedRegistration(
	ctx context.Context,
	q FederatedRegistrationInserter,
	snapshot FederatedIdentitySnapshot,
	ttl time.Duration,
) (string, time.Time, error) {
	if err := validateFederatedIdentitySnapshot(snapshot); err != nil {
		return "", time.Time{}, err
	}
	if ttl == 0 {
		ttl = FederatedEnrollmentTTL
	}
	token, err := newEnrollmentToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(ttl)
	avatarURL := pgtype.Text{}
	if snapshot.AvatarURL != nil {
		avatarURL = pgtype.Text{String: *snapshot.AvatarURL, Valid: true}
	}
	if _, err := q.InsertFederatedRegistrationEnrollment(ctx, db.InsertFederatedRegistrationEnrollmentParams{
		Token:                    token,
		ExpiresAt:                pgtype.Timestamptz{Time: expiresAt, Valid: true},
		FederatedUpstreamIdpID:   pgtype.Int8{Int64: snapshot.UpstreamIDPID, Valid: true},
		FederatedUpstreamIdpSlug: pgtype.Text{String: snapshot.UpstreamIDPSlug, Valid: true},
		FederatedUpstreamIss:     pgtype.Text{String: snapshot.Issuer, Valid: true},
		FederatedUpstreamSub:     pgtype.Text{String: snapshot.Subject, Valid: true},
		FederatedDisplayName:     pgtype.Text{String: snapshot.DisplayName, Valid: true},
		FederatedUpstreamData:    snapshot.UpstreamData,
		FederatedAvatarUrl:       avatarURL,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("enrollment: insert federated registration: %w", err)
	}
	return token, expiresAt, nil
}

// IssueProviderRecovery stores a short-lived reset enrollment bound to the
// account and provider that verified the recovery identity.
func IssueProviderRecovery(
	ctx context.Context,
	q ProviderRecoveryInserter,
	targetAccountID int32,
	sourceUpstreamIDPID int64,
	ttl time.Duration,
) (string, time.Time, error) {
	if ttl == 0 {
		ttl = FederatedEnrollmentTTL
	}
	token, err := newEnrollmentToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(ttl)
	if _, err := q.InsertProviderRecoveryEnrollment(ctx, db.InsertProviderRecoveryEnrollmentParams{
		Token:                       token,
		TargetAccountID:             pgtype.Int4{Int32: targetAccountID, Valid: true},
		ExpiresAt:                   pgtype.Timestamptz{Time: expiresAt, Valid: true},
		RecoverySourceUpstreamIdpID: pgtype.Int8{Int64: sourceUpstreamIDPID, Valid: true},
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("enrollment: insert provider recovery: %w", err)
	}
	return token, expiresAt, nil
}

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
		if tpl.ExpectedUpstreamIDPSlug != nil {
			params.ExpectedUpstreamIdpSlug = pgtype.Text{String: *tpl.ExpectedUpstreamIDPSlug, Valid: true}
		}
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
