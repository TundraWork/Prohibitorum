package federation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	credentialenrollment "prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
)

// EnrollmentIssuanceQueries is the narrow persistence surface required to
// classify a verified identity and issue its enrollment.
type EnrollmentIssuanceQueries interface {
	GetAccountIdentityByIssuerSub(context.Context, db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error)
	GetAccountByID(context.Context, int32) (db.Account, error)
	InsertFederatedRegistrationEnrollment(context.Context, db.InsertFederatedRegistrationEnrollmentParams) (db.Enrollment, error)
	InsertProviderRecoveryEnrollment(context.Context, db.InsertProviderRecoveryEnrollmentParams) (db.Enrollment, error)
}

// VRChatEnrollmentIssuer classifies adapter-verified VRChat identities into
// registration or provider-recovery enrollments.
type VRChatEnrollmentIssuer struct {
	queries EnrollmentIssuanceQueries
	audit   audit.Writer
}

func NewVRChatEnrollmentIssuer(queries EnrollmentIssuanceQueries, writer audit.Writer) *VRChatEnrollmentIssuer {
	return &VRChatEnrollmentIssuer{queries: queries, audit: writer}
}

func (i *VRChatEnrollmentIssuer) recordIssued(ctx context.Context, provider Provider, identity VerifiedIdentity, intent string) {
	audit.RecordOrLog(ctx, i.audit, audit.Record{
		Factor: audit.FactorEnrollment,
		Event:  audit.EventEnrollmentIssued,
		Detail: map[string]any{
			"intent":       intent,
			"idp_slug":     provider.Slug,
			"upstream_iss": identity.Issuer,
			"upstream_sub": identity.Subject,
		},
	})
}

func (i *VRChatEnrollmentIssuer) Issue(ctx context.Context, provider Provider, identity VerifiedIdentity) (EnrollmentGrant, error) {
	if provider.Protocol != "vrchat" || provider.Mode != ModeLinkOnly {
		return EnrollmentGrant{}, authn.ErrFederationStateInvalid()
	}
	upstreamData, err := ValidateUpstreamData(identity.UpstreamData)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	stored, err := i.queries.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
		UpstreamIss: identity.Issuer,
		UpstreamSub: identity.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		var avatarURL *string
		if identity.AvatarURL != "" {
			avatarURL = &identity.AvatarURL
		}
		token, expiresAt, err := credentialenrollment.IssueFederatedRegistration(ctx, i.queries, credentialenrollment.FederatedIdentitySnapshot{
			UpstreamIDPID:   provider.ID,
			UpstreamIDPSlug: provider.Slug,
			Issuer:          identity.Issuer,
			Subject:         identity.Subject,
			DisplayName:     identity.DisplayName,
			UpstreamData:    upstreamData,
			AvatarURL:       avatarURL,
		}, 0)
		if err != nil {
			return EnrollmentGrant{}, err
		}
		i.recordIssued(ctx, provider, identity, credentialenrollment.IntentFederatedRegister)
		return EnrollmentGrant{Token: token, Intent: credentialenrollment.IntentFederatedRegister, ExpiresAt: expiresAt}, nil
	}
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("federation: lookup enrollment identity: %w", err)
	}
	if stored.UpstreamIdpID != provider.ID {
		return EnrollmentGrant{}, authn.ErrFederationStateInvalid()
	}
	account, err := i.queries.GetAccountByID(ctx, stored.AccountID)
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("federation: load recovery account: %w", err)
	}
	if account.Disabled {
		return EnrollmentGrant{}, authn.ErrBadCredentials()
	}
	token, expiresAt, err := credentialenrollment.IssueProviderRecovery(ctx, i.queries, account.ID, provider.ID, 0)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	i.recordIssued(ctx, provider, identity, credentialenrollment.IntentReset)
	return EnrollmentGrant{Token: token, Intent: credentialenrollment.IntentReset, ExpiresAt: expiresAt}, nil
}
