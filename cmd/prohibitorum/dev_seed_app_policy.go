package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
)

const (
	appPolicyDemoClientID   = "dev-app"
	appPolicyDemoManualSlug = "demo-manual-access"
)

// appPolicyDemoGroup describes one closed rule-group fixture row.
type appPolicyDemoGroup struct {
	slug        string
	displayName string
	description string
	exposed     bool
	rule        appaccess.Rule
}

func appPolicyDemoGroups() []appPolicyDemoGroup {
	condition := func(fact string, fields appaccess.Condition) appaccess.Condition {
		fields.Fact = fact
		return fields
	}
	all := func(children ...appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "all", Children: children}
	}
	any := func(children ...appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "any", Children: children}
	}
	not := func(child appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "not", Child: &child}
	}
	providerDownstream := condition("connection.provider", appaccess.Condition{Provider: "downstream-policy-demo"})
	protocolOIDC := condition("connection.protocol", appaccess.Condition{Protocol: "oidc"})
	passkey := condition("login_method", appaccess.Condition{Method: "passkey"})
	passwordTOTP := condition("login_method", appaccess.Condition{Method: "password_totp"})
	federation := condition("login_method", appaccess.Condition{Method: "federation"})
	anyAvatar := condition("avatar", appaccess.Condition{Source: "any"})
	userAvatar := condition("avatar", appaccess.Condition{Source: "user_uploaded"})
	rule := func(condition appaccess.Condition) appaccess.Rule {
		return appaccess.Rule{Version: 1, Condition: condition}
	}

	return []appPolicyDemoGroup{
		{slug: "demo-downstream-connected", displayName: "Downstream connected", description: "Accounts with a confirmed downstream connection.", exposed: true, rule: rule(providerDownstream)},
		{slug: "demo-oidc-connected", displayName: "OIDC connected", description: "Accounts with a confirmed OIDC connection.", exposed: true, rule: rule(protocolOIDC)},
		{slug: "demo-passkey-login", displayName: "Passkey login", description: "Accounts with a passkey credential.", exposed: true, rule: rule(passkey)},
		{slug: "demo-password-totp-login", displayName: "Password and TOTP login", description: "Accounts with password and confirmed TOTP credentials.", exposed: true, rule: rule(passwordTOTP)},
		{slug: "demo-federation-login", displayName: "Federation login", description: "Accounts eligible for federation login.", exposed: true, rule: rule(federation)},
		{slug: "demo-any-avatar", displayName: "Any avatar", description: "Accounts with an avatar from any source.", exposed: true, rule: rule(anyAvatar)},
		{slug: "demo-user-avatar", displayName: "User avatar", description: "Accounts with a user-uploaded avatar.", exposed: true, rule: rule(userAvatar)},
		{slug: "demo-all-strong-profile", displayName: "All strong profile facts", description: "Accounts with a passkey and user-uploaded avatar.", exposed: true, rule: rule(all(passkey, userAvatar))},
		{slug: "demo-any-strong-login", displayName: "Any strong login", description: "Accounts with a passkey or password plus TOTP login.", exposed: true, rule: rule(any(passkey, passwordTOTP))},
		{slug: "demo-no-user-avatar", displayName: "No user avatar", description: "Accounts without a user-uploaded avatar.", exposed: false, rule: rule(not(userAvatar))},
		{slug: "demo-trusted-federated-profile", displayName: "Trusted federated profile", description: "Downstream OIDC accounts with federation or passkey login but no password plus TOTP login.", exposed: true, rule: rule(all(providerDownstream, protocolOIDC, any(federation, passkey), not(passwordTOTP)))},
	}
}

var appPolicyDemoPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x60, 0x00, 0x02, 0x00,
	0x00, 0x05, 0x00, 0x01, 0x7a, 0x5e, 0xab, 0x3f,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
}

func seedAppPolicyDemo(ctx context.Context, pool *pgxpool.Pool, cfg configx.Config) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("app-policy demo begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := db.New(tx)

	if _, err := q.GetOIDCClientAny(ctx, appPolicyDemoClientID); err != nil {
		return appPolicyDemoPrerequisiteError(appPolicyDemoClientID, err)
	}
	accounts := make(map[string]db.Account, 4)
	for _, username := range []string{"alice", "bob", "carol", "dave"} {
		account, err := q.GetAccountByUsername(ctx, username)
		if err != nil {
			return appPolicyDemoPrerequisiteError(username, err)
		}
		accounts[username] = account
	}
	provider, err := q.GetUpstreamIDPBySlugAny(ctx, policyDemoUpstreamIDPSlug)
	if err != nil {
		return appPolicyDemoPrerequisiteError(policyDemoUpstreamIDPSlug, err)
	}
	if provider.Disabled {
		return fmt.Errorf("app-policy demo prerequisite %q must be enabled", policyDemoUpstreamIDPSlug)
	}
	if provider.Protocol != "oidc" {
		return fmt.Errorf("app-policy demo prerequisite %q protocol = %q, want oidc", policyDemoUpstreamIDPSlug, provider.Protocol)
	}
	existingGroups, err := q.ListOIDCAppGroups(ctx, appPolicyDemoClientID)
	if err != nil {
		return fmt.Errorf("app-policy demo list groups for %q: %w", appPolicyDemoClientID, err)
	}
	groupsBySlug := make(map[string]db.UserGroup, len(existingGroups))
	for _, group := range existingGroups {
		groupsBySlug[group.Slug] = group
	}

	if err := seedAppPolicyDemoAlice(ctx, q, accounts["alice"], provider); err != nil {
		return err
	}
	if err := seedAppPolicyDemoBob(ctx, q, accounts["bob"], provider, cfg); err != nil {
		return err
	}
	if err := seedAppPolicyDemoApp(ctx, q, accounts["alice"], accounts["bob"], accounts["carol"], groupsBySlug); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("app-policy demo commit transaction: %w", err)
	}
	return nil
}
func appPolicyDemoPrerequisiteError(identifier string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("app-policy demo prerequisite %q not found", identifier)
	}
	return fmt.Errorf("app-policy demo load prerequisite %q: %w", identifier, err)
}

func seedAppPolicyDemoAlice(ctx context.Context, q *db.Queries, alice db.Account, provider db.UpstreamIdp) error {
	if _, err := q.UpdateAccount(ctx, db.UpdateAccountParams{
		ID:            alice.ID,
		DisplayName:   alice.DisplayName,
		Role:          "app_manager",
		Attributes:    alice.Attributes,
		Disabled:      alice.Disabled,
		Email:         alice.Email,
		EmailVerified: alice.EmailVerified,
	}); err != nil {
		return fmt.Errorf("app-policy demo update alice role: %w", err)
	}

	credentials, err := q.ListCredentialsByAccount(ctx, alice.ID)
	if err != nil {
		return fmt.Errorf("app-policy demo list alice credentials: %w", err)
	}
	if len(credentials) == 0 {
		credentialID, err := appPolicyDemoRandomBytes(32)
		if err != nil {
			return fmt.Errorf("app-policy demo generate alice credential ID: %w", err)
		}
		publicKey, err := appPolicyDemoRandomBytes(32)
		if err != nil {
			return fmt.Errorf("app-policy demo generate alice public key: %w", err)
		}
		if _, err := q.InsertCredential(ctx, db.InsertCredentialParams{
			AccountID:       alice.ID,
			CredentialID:    credentialID,
			PublicKey:       publicKey,
			CoseAlg:         -7,
			UserHandle:      alice.WebauthnUserHandle,
			Nickname:        pgtype.Text{String: "App policy demo fixture", Valid: true},
			AttestationType: pgtype.Text{String: "none", Valid: true},
		}); err != nil {
			return fmt.Errorf("app-policy demo insert alice credential: %w", err)
		}
	}

	identities, err := q.ListAccountIdentitiesByAccount(ctx, alice.ID)
	if err != nil {
		return fmt.Errorf("app-policy demo list alice identities: %w", err)
	}
	var identityID int64
	for _, identity := range identities {
		if identity.UpstreamIdpID == provider.ID {
			identityID = identity.ID
			break
		}
	}
	if identityID == 0 {
		var providerConfig federationoidc.Config
		if err := json.Unmarshal(provider.ProviderConfig, &providerConfig); err != nil {
			return fmt.Errorf("app-policy demo parse provider %q config: %w", provider.Slug, err)
		}
		identity, err := q.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{AccountID: alice.ID, UpstreamIdpID: provider.ID, UpstreamIss: providerConfig.IssuerURL, UpstreamSub: "dev-seed-app-policy-alice", UpstreamEmail: pgtype.Text{String: "alice@example.com", Valid: true}, UpstreamData: []byte("{}")})
		if err != nil {
			return fmt.Errorf("app-policy demo insert alice identity: %w", err)
		}
		identityID = identity.ID
	}
	if err := q.ConfirmAccountIdentity(ctx, identityID); err != nil {
		return fmt.Errorf("app-policy demo confirm alice identity: %w", err)
	}
	userAvatarExists, err := appPolicyDemoAvatarSourceExists(ctx, q, alice.ID, "user")
	if err != nil {
		return fmt.Errorf("app-policy demo list alice avatar sources: %w", err)
	}
	if !userAvatarExists {
		if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
			AccountID:   alice.ID,
			Source:      "user",
			Bytes:       appPolicyDemoPNG,
			ContentType: pgtype.Text{String: "image/png", Valid: true},
			Etag:        appPolicyDemoETag(),
		}); err != nil {
			return fmt.Errorf("app-policy demo insert alice user avatar: %w", err)
		}
	}
	return nil
}

func seedAppPolicyDemoBob(ctx context.Context, q *db.Queries, bob db.Account, provider db.UpstreamIdp, cfg configx.Config) error {
	if _, err := q.GetPasswordCredential(ctx, bob.ID); errors.Is(err, pgx.ErrNoRows) {
		secret, err := appPolicyDemoRandomBytes(32)
		if err != nil {
			return fmt.Errorf("app-policy demo generate bob password material: %w", err)
		}
		hash, err := password.HashRaw(base64.RawURLEncoding.EncodeToString(secret), cfg.PasswordHashParams)
		if err != nil {
			return fmt.Errorf("app-policy demo hash bob password material: %w", err)
		}
		if err := q.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{AccountID: bob.ID, Hash: hash}); err != nil {
			return fmt.Errorf("app-policy demo insert bob password credential: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("app-policy demo get bob password credential: %w", err)
	}

	_, err := q.GetTOTPCredential(ctx, bob.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		ciphertext, err := appPolicyDemoRandomBytes(32)
		if err != nil {
			return fmt.Errorf("app-policy demo generate bob TOTP ciphertext: %w", err)
		}
		nonce, err := appPolicyDemoRandomBytes(12)
		if err != nil {
			return fmt.Errorf("app-policy demo generate bob TOTP nonce: %w", err)
		}
		period := cfg.TOTP.DefaultPeriod
		if period == 0 {
			period = 30
		}
		digits := cfg.TOTP.DefaultDigits
		if digits == 0 {
			digits = 6
		}
		algorithm := cfg.TOTP.DefaultAlgorithm
		if algorithm == "" {
			algorithm = "SHA1"
		}
		if _, err := q.InsertTOTPCredential(ctx, db.InsertTOTPCredentialParams{
			AccountID:   bob.ID,
			SecretEnc:   ciphertext,
			SecretNonce: nonce,
			KeyVersion:  1,
			Period:      int32(period),
			Digits:      int32(digits),
			Algorithm:   algorithm,
		}); err != nil {
			return fmt.Errorf("app-policy demo insert bob TOTP credential: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("app-policy demo get bob TOTP credential: %w", err)
	}
	if err := q.ConfirmTOTPCredential(ctx, bob.ID); err != nil {
		return fmt.Errorf("app-policy demo confirm bob TOTP credential: %w", err)
	}
	providerAvatarSource := "upstream:" + provider.Slug
	providerAvatarExists, err := appPolicyDemoAvatarSourceExists(ctx, q, bob.ID, providerAvatarSource)
	if err != nil {
		return fmt.Errorf("app-policy demo list bob avatar sources: %w", err)
	}
	if !providerAvatarExists {
		providerID := provider.ID
		if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
			AccountID:   bob.ID,
			Source:      providerAvatarSource,
			Bytes:       appPolicyDemoPNG,
			ContentType: pgtype.Text{String: "image/png", Valid: true},
			Etag:        appPolicyDemoETag(),
			IdpID:       &providerID,
		}); err != nil {
			return fmt.Errorf("app-policy demo insert bob provider avatar: %w", err)
		}
	}
	return nil
}

func appPolicyDemoAvatarSourceExists(ctx context.Context, q *db.Queries, accountID int32, source string) (bool, error) {
	sources, err := q.ListAvatarSourcesByAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, avatarSource := range sources {
		if avatarSource.Source == source {
			return true, nil
		}
	}
	return false, nil
}

func seedAppPolicyDemoApp(ctx context.Context, q *db.Queries, alice, bob, carol db.Account, bySlug map[string]db.UserGroup) error {
	if err := q.AssignOIDCClientManager(ctx, db.AssignOIDCClientManagerParams{ClientID: appPolicyDemoClientID, AccountID: alice.ID}); err != nil {
		return fmt.Errorf("app-policy demo assign alice manager for %q: %w", appPolicyDemoClientID, err)
	}
	if _, err := q.SetOIDCClientAccessRestricted(ctx, db.SetOIDCClientAccessRestrictedParams{ClientID: appPolicyDemoClientID, AccessRestricted: true}); err != nil {
		return fmt.Errorf("app-policy demo restrict %q: %w", appPolicyDemoClientID, err)
	}
	providerSlugs, err := q.ListKnownUpstreamIDPSlugs(ctx)
	if err != nil {
		return fmt.Errorf("app-policy demo list known providers: %w", err)
	}
	knownProviders := make(map[string]struct{}, len(providerSlugs))
	for _, slug := range providerSlugs {
		knownProviders[slug] = struct{}{}
	}
	canonicalRules := make(map[string][]byte, len(appPolicyDemoGroups()))
	for _, group := range appPolicyDemoGroups() {
		raw, err := json.Marshal(group.rule)
		if err != nil {
			return fmt.Errorf("app-policy demo marshal rule %q: %w", group.slug, err)
		}
		validated, err := appaccess.ParseAndValidateRule(raw, knownProviders)
		if err != nil {
			return fmt.Errorf("app-policy demo validate rule %q: %w", group.slug, err)
		}
		canonical, err := json.Marshal(validated)
		if err != nil {
			return fmt.Errorf("app-policy demo marshal validated rule %q: %w", group.slug, err)
		}
		canonicalRules[group.slug] = canonical
	}

	for _, group := range appPolicyDemoGroups() {
		if _, err := upsertAppPolicyDemoGroup(ctx, q, bySlug, group.slug, group.displayName, group.description, group.exposed, "rule", canonicalRules[group.slug]); err != nil {
			return err
		}
	}
	manual, err := upsertAppPolicyDemoGroup(ctx, q, bySlug, appPolicyDemoManualSlug, "Manual access", "Explicit per-account access decisions.", true, "manual", nil)
	if err != nil {
		return err
	}
	for _, decision := range []struct {
		account db.Account
		effect  string
	}{
		{account: bob, effect: "allow"},
		{account: carol, effect: "deny"},
	} {
		if _, err := q.UpsertManualDecision(ctx, db.UpsertManualDecisionParams{GroupID: manual.ID, AccountID: decision.account.ID, Effect: decision.effect}); err != nil {
			return fmt.Errorf("app-policy demo upsert %s manual decision: %w", decision.account.Username, err)
		}
	}
	return nil
}

func upsertAppPolicyDemoGroup(ctx context.Context, q *db.Queries, existing map[string]db.UserGroup, slug, displayName, description string, exposed bool, kind string, rule []byte) (db.UserGroup, error) {
	group, found := existing[slug]
	if found && group.Kind != kind {
		return db.UserGroup{}, fmt.Errorf("app-policy demo group %q has wrong kind %q, want %q", slug, group.Kind, kind)
	}
	params := db.UpdateAppGroupParams{
		Slug:                slug,
		DisplayName:         displayName,
		Description:         pgtype.Text{String: description, Valid: true},
		ExposedToDownstream: exposed,
		Rule:                rule,
		OidcClientID:        pgtype.Text{String: appPolicyDemoClientID, Valid: true},
	}
	if found {
		params.GroupID = group.ID
		updated, err := q.UpdateAppGroup(ctx, params)
		if err != nil {
			return db.UserGroup{}, fmt.Errorf("app-policy demo update %s group %q: %w", kind, slug, err)
		}
		return updated, nil
	}
	created, err := q.CreateOIDCAppGroup(ctx, db.CreateOIDCAppGroupParams{
		Kind:                kind,
		Slug:                slug,
		DisplayName:         displayName,
		Description:         params.Description,
		ExposedToDownstream: exposed,
		Rule:                rule,
		OidcClientID:        appPolicyDemoClientID,
	})
	if err != nil {
		return db.UserGroup{}, fmt.Errorf("app-policy demo create %s group %q: %w", kind, slug, err)
	}
	return created, nil
}

func appPolicyDemoRandomBytes(size int) ([]byte, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

func appPolicyDemoETag() pgtype.Text {
	digest := sha256.Sum256(appPolicyDemoPNG)
	return pgtype.Text{String: hex.EncodeToString(digest[:]), Valid: true}
}
