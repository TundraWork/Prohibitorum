package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	protocoloidc "prohibitorum/pkg/protocol/oidc"
)

func appPolicyDemoTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	basePool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}

	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		basePool.Close()
		t.Fatal(err)
	}
	schema := "app_policy_demo_" + hex.EncodeToString(nonce[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := basePool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		basePool.Close()
		t.Fatal(err)
	}
	cleanupSchema := func() {
		_, _ = basePool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		basePool.Close()
	}
	if _, err := basePool.Exec(ctx, `CREATE TABLE `+quotedSchema+`.goose_db_version (
		id serial PRIMARY KEY,
		version_id bigint NOT NULL,
		is_applied boolean NOT NULL,
		tstamp timestamp NULL DEFAULT now()
	)`); err != nil {
		cleanupSchema()
		t.Fatalf("create schema migration metadata: %v", err)
	}
	if _, err := basePool.Exec(ctx, `INSERT INTO `+quotedSchema+`.goose_db_version (version_id, is_applied) VALUES (0, true)`); err != nil {
		cleanupSchema()
		t.Fatalf("initialize schema migration metadata: %v", err)
	}

	schemaURL, err := url.Parse(baseURL)
	if err != nil {
		cleanupSchema()
		t.Fatal(err)
	}
	query := schemaURL.Query()
	query.Set("search_path", schema+",public")
	schemaURL.RawQuery = query.Encode()
	if _, err := migrations.UpWithResult(schemaURL.String()); err != nil {
		cleanupSchema()
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, schemaURL.String())
	if err != nil {
		cleanupSchema()
		t.Fatal(err)
	}
	cleanup := func() {
		pool.Close()
		cleanupSchema()
	}
	q := db.New(pool)

	providerConfig, err := json.Marshal(federationoidc.Config{
		IssuerURL:             "https://downstream.example.test",
		ClientID:              "upstream-policy-demo-federation",
		Scopes:                []string{"openid", "email", "profile"},
		AllowedDomains:        []string{},
		UsernameClaim:         "preferred_username",
		DisplayNameClaim:      "name",
		EmailClaim:            "email",
		PictureClaim:          "picture",
		RequireVerifiedEmail:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertUpstreamIDP(ctx, db.InsertUpstreamIDPParams{
		Slug:           "downstream-policy-demo",
		DisplayName:    "Downstream policy demo",
		Protocol:       federationoidc.Protocol,
		Mode:           "auto_provision",
		SecretStatus:   "unconfigured",
		ProviderConfig: providerConfig,
	}); err != nil {
		t.Fatalf("insert downstream policy demo provider: %v", err)
	}

	clientParams, _, err := protocoloidc.BuildClientParams(protocoloidc.ClientOptions{
		ClientID:       appPolicyDemoClientID,
		DisplayName:    "Example App (dev)",
		RedirectURIs:   []string{"https://app.localhost/callback"},
		Scopes:         []string{"openid", "profile", "email"},
		Public:         true,
		RequireConsent: true,
		RequirePKCE:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertOIDCClient(ctx, clientParams); err != nil {
		t.Fatalf("insert OIDC client: %v", err)
	}

	for i, account := range []struct {
		username string
		disabled bool
	}{
		{username: "alice"},
		{username: "bob"},
		{username: "carol"},
		{username: "dave", disabled: true},
	} {
		handle := make([]byte, 16)
		handle[len(handle)-1] = byte(i + 1)
		if _, err := q.InsertAccount(ctx, db.InsertAccountParams{
			Username:           account.username,
			DisplayName:        strings.ToUpper(account.username[:1]) + account.username[1:],
			WebauthnUserHandle: handle,
			Role:               "user",
			Attributes:         []byte(`{"fixture":true}`),
			Disabled:           account.disabled,
			Email:              pgtype.Text{String: account.username + "@example.com", Valid: true},
			EmailVerified:      true,
		}); err != nil {
			t.Fatalf("insert %s: %v", account.username, err)
		}
	}
	return pool, cleanup
}

func TestSeedProvidersSkipsExistingDisabledProvider(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := appPolicyDemoTestPool(t)
	t.Cleanup(cleanup)
	q := db.New(pool)
	if _, err := q.InsertUpstreamIDP(ctx, db.InsertUpstreamIDPParams{
		Slug: "google", DisplayName: "Existing Google", Protocol: federationoidc.Protocol,
		Mode: "auto_provision", SecretStatus: "unconfigured", Disabled: true,
		ProviderConfig: []byte(`{"issuer_url":"https://existing.example.test"}`),
	}); err != nil {
		t.Fatalf("insert existing disabled provider: %v", err)
	}

	seedProviders(ctx, q)

	provider, err := q.GetUpstreamIDPBySlugAny(ctx, "google")
	if err != nil {
		t.Fatalf("get existing provider: %v", err)
	}
	if !provider.Disabled || provider.DisplayName != "Existing Google" {
		t.Fatalf("existing disabled provider mutated: %+v", provider)
	}
}

func appPolicyDemoConfig() configx.Config {
	return configx.Config{PasswordHashParams: password.DefaultParams()}
}

func appPolicyDemoAccounts(t *testing.T, q *db.Queries) map[string]db.Account {
	t.Helper()
	accounts := make(map[string]db.Account, 4)
	for _, username := range []string{"alice", "bob", "carol", "dave"} {
		account, err := q.GetAccountByUsername(context.Background(), username)
		if err != nil {
			t.Fatalf("get %s: %v", username, err)
		}
		accounts[username] = account
	}
	return accounts
}

func TestAppPolicyDemoPNGIsValid(t *testing.T) {
	if _, err := png.Decode(bytes.NewReader(appPolicyDemoPNG)); err != nil {
		t.Fatalf("decode app policy demo PNG: %v", err)
	}
}

func appPolicyDemoGroupMap(t *testing.T, q *db.Queries) map[string]db.UserGroup {
	t.Helper()
	groups, err := q.ListOIDCAppGroups(context.Background(), appPolicyDemoClientID)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	result := make(map[string]db.UserGroup, len(groups))
	for _, group := range groups {
		result[group.Slug] = group
	}
	return result
}

func appPolicyDemoExpectedRules() map[string]appaccess.Rule {
	all := func(children ...appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "all", Children: children}
	}
	any := func(children ...appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "any", Children: children}
	}
	not := func(child appaccess.Condition) appaccess.Condition {
		return appaccess.Condition{Op: "not", Child: &child}
	}
	provider := appaccess.Condition{Fact: "connection.provider", Provider: policyDemoUpstreamIDPSlug}
	protocol := appaccess.Condition{Fact: "connection.protocol", Protocol: "oidc"}
	passkey := appaccess.Condition{Fact: "login_method", Method: "passkey"}
	passwordTOTP := appaccess.Condition{Fact: "login_method", Method: "password_totp"}
	federation := appaccess.Condition{Fact: "login_method", Method: "federation"}
	anyAvatar := appaccess.Condition{Fact: "avatar", Source: "any"}
	userAvatar := appaccess.Condition{Fact: "avatar", Source: "user_uploaded"}
	rule := func(condition appaccess.Condition) appaccess.Rule {
		return appaccess.Rule{Version: 1, Condition: condition}
	}
	return map[string]appaccess.Rule{
		"demo-downstream-connected":      rule(provider),
		"demo-oidc-connected":            rule(protocol),
		"demo-passkey-login":             rule(passkey),
		"demo-password-totp-login":       rule(passwordTOTP),
		"demo-federation-login":          rule(federation),
		"demo-any-avatar":                rule(anyAvatar),
		"demo-user-avatar":               rule(userAvatar),
		"demo-all-strong-profile":        rule(all(passkey, userAvatar)),
		"demo-any-strong-login":          rule(any(passkey, passwordTOTP)),
		"demo-no-user-avatar":            rule(not(userAvatar)),
		"demo-trusted-federated-profile": rule(all(provider, protocol, any(federation, passkey), not(passwordTOTP))),
	}
}

func TestSeedAppPolicyDemoCreatesCompleteShowcase(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := appPolicyDemoTestPool(t)
	t.Cleanup(cleanup)
	q := db.New(pool)
	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err != nil {
		t.Fatalf("seed app policy demo: %v", err)
	}
	accounts := appPolicyDemoAccounts(t, q)

	if got := accounts["alice"].Role; got != "app_manager" {
		t.Fatalf("alice role = %q, want app_manager", got)
	}
	client, err := q.GetOIDCClient(ctx, appPolicyDemoClientID)
	if err != nil {
		t.Fatal(err)
	}
	if !client.AccessRestricted {
		t.Fatal("dev-app access_restricted = false, want true")
	}
	managers, err := q.ListOIDCClientManagers(ctx, appPolicyDemoClientID)
	if err != nil {
		t.Fatal(err)
	}
	if len(managers) != 1 || managers[0].AccountID != accounts["alice"].ID {
		t.Fatalf("manager assignments = %+v, want only alice", managers)
	}

	groups := appPolicyDemoGroupMap(t, q)
	manual, ok := groups[appPolicyDemoManualSlug]
	if !ok || manual.Kind != "manual" {
		t.Fatalf("manual group = %+v, want %q kind manual", manual, appPolicyDemoManualSlug)
	}
	for username, wantEffect := range map[string]string{"bob": "allow", "carol": "deny"} {
		decision, err := q.GetManualDecisionForOIDCApp(ctx, db.GetManualDecisionForOIDCAppParams{OidcClientID: appPolicyDemoClientID, AccountID: accounts[username].ID})
		if err != nil {
			t.Fatalf("get manual decision for %s: %v", username, err)
		}
		if decision.Effect != wantEffect || decision.GroupID != manual.ID {
			t.Fatalf("manual decision for %s = %+v, want %s on group %d", username, decision, wantEffect, manual.ID)
		}
	}
	if _, err := q.GetManualDecisionForOIDCApp(ctx, db.GetManualDecisionForOIDCAppParams{OidcClientID: appPolicyDemoClientID, AccountID: accounts["alice"].ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("alice manual decision error = %v, want pgx.ErrNoRows", err)
	}

	expectedExposure := map[string]bool{
		"demo-downstream-connected":      true,
		"demo-oidc-connected":            true,
		"demo-passkey-login":             true,
		"demo-password-totp-login":       true,
		"demo-federation-login":          true,
		"demo-any-avatar":                true,
		"demo-user-avatar":               true,
		"demo-all-strong-profile":        true,
		"demo-any-strong-login":          true,
		"demo-no-user-avatar":            false,
		"demo-trusted-federated-profile": true,
	}
	if len(groups) != len(expectedExposure)+1 {
		t.Fatalf("group count = %d, want %d", len(groups), len(expectedExposure)+1)
	}
	knownProviders := map[string]struct{}{policyDemoUpstreamIDPSlug: {}}
	expectedRules := appPolicyDemoExpectedRules()
	for slug, exposed := range expectedExposure {
		group, ok := groups[slug]
		if !ok {
			t.Errorf("missing rule group %q", slug)
			continue
		}
		if group.Kind != "rule" || group.ExposedToDownstream != exposed {
			t.Errorf("group %q = kind %q exposed %v, want rule/%v", slug, group.Kind, group.ExposedToDownstream, exposed)
		}
		parsed, err := appaccess.ParseAndValidateRule(group.Rule, knownProviders)
		if err != nil {
			t.Errorf("parse persisted rule %q: %v", slug, err)
			continue
		}
		if want := expectedRules[slug]; !reflect.DeepEqual(parsed, want) {
			t.Errorf("canonical rule %q = %#v, want %#v", slug, parsed, want)
		}
	}

	assertFacts := func(username string, want db.GetAccountAccessFactsRow) {
		t.Helper()
		got, err := q.GetAccountAccessFacts(ctx, accounts[username].ID)
		if err != nil {
			t.Fatalf("get facts for %s: %v", username, err)
		}
		if got.HasPasskey != want.HasPasskey || got.HasPasswordTotp != want.HasPasswordTotp || got.HasFederation != want.HasFederation || got.HasAnyAvatar != want.HasAnyAvatar || got.HasUserAvatar != want.HasUserAvatar || strings.Join(got.ConfirmedProviderSlugs, ",") != strings.Join(want.ConfirmedProviderSlugs, ",") || strings.Join(got.ConfirmedProtocols, ",") != strings.Join(want.ConfirmedProtocols, ",") {
			t.Errorf("facts for %s = %+v, want %+v", username, got, want)
		}
	}
	assertFacts("alice", db.GetAccountAccessFactsRow{HasPasskey: true, HasFederation: true, ConfirmedProviderSlugs: []string{"downstream-policy-demo"}, ConfirmedProtocols: []string{"oidc"}, HasAnyAvatar: true, HasUserAvatar: true})
	assertFacts("bob", db.GetAccountAccessFactsRow{HasPasswordTotp: true, HasAnyAvatar: true})
	assertFacts("carol", db.GetAccountAccessFactsRow{})

	matrix := map[string]map[string]bool{
		"demo-downstream-connected":      {"alice": true, "bob": false, "carol": false},
		"demo-oidc-connected":            {"alice": true, "bob": false, "carol": false},
		"demo-passkey-login":             {"alice": true, "bob": false, "carol": false},
		"demo-password-totp-login":       {"alice": false, "bob": true, "carol": false},
		"demo-federation-login":          {"alice": true, "bob": false, "carol": false},
		"demo-any-avatar":                {"alice": true, "bob": true, "carol": false},
		"demo-user-avatar":               {"alice": true, "bob": false, "carol": false},
		"demo-all-strong-profile":        {"alice": true, "bob": false, "carol": false},
		"demo-any-strong-login":          {"alice": true, "bob": true, "carol": false},
		"demo-no-user-avatar":            {"alice": false, "bob": true, "carol": true},
		"demo-trusted-federated-profile": {"alice": true, "bob": false, "carol": false},
	}
	service := appaccess.NewService(q)
	for slug, expected := range matrix {
		previews, err := service.PreviewGroup(ctx, appaccess.AppRef{Kind: appaccess.KindOIDC, OIDCClientID: appPolicyDemoClientID}, groups[slug].ID, db.ListActiveAccountAccessFactsPageParams{RowLimit: 100})
		if err != nil {
			t.Fatalf("preview %s: %v", slug, err)
		}
		if len(previews) != 3 {
			t.Fatalf("preview %s count = %d, want 3 active fixture accounts", slug, len(previews))
		}
		for _, preview := range previews {
			if preview.Account.Username == "dave" {
				t.Fatalf("preview %s includes disabled dave", slug)
			}
			want, ok := expected[preview.Account.Username]
			if !ok || preview.Matched != want {
				t.Errorf("preview %s for %s = %v, want %v", slug, preview.Account.Username, preview.Matched, want)
			}
		}
	}
}

type appPolicyDemoAvatarState struct {
	bytes       []byte
	contentType pgtype.Text
	etag        pgtype.Text
	idpID       pgtype.Int8
}

type appPolicyDemoActiveAvatarState struct {
	source      pgtype.Text
	contentType pgtype.Text
	etag        pgtype.Text
}

func appPolicyDemoAvatarSourceState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID int32, source string) appPolicyDemoAvatarState {
	t.Helper()
	var state appPolicyDemoAvatarState
	if err := pool.QueryRow(ctx, `
		SELECT bytes, content_type, etag, idp_id
		FROM account_avatar
		WHERE account_id = $1 AND source = $2`, accountID, source,
	).Scan(&state.bytes, &state.contentType, &state.etag, &state.idpID); err != nil {
		t.Fatalf("load avatar source %q: %v", source, err)
	}
	return state
}

func appPolicyDemoActiveAvatarStateFor(t *testing.T, ctx context.Context, q *db.Queries, accountID int32) appPolicyDemoActiveAvatarState {
	t.Helper()
	account, err := q.GetAccountByID(ctx, accountID)
	if err != nil {
		t.Fatalf("load account %d: %v", accountID, err)
	}
	return appPolicyDemoActiveAvatarState{
		source:      account.AvatarSource,
		contentType: account.AvatarContentType,
		etag:        account.AvatarEtag,
	}
}

func TestSeedAppPolicyDemoPreservesExistingAvatarSources(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := appPolicyDemoTestPool(t)
	t.Cleanup(cleanup)
	q := db.New(pool)
	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err != nil {
		t.Fatalf("initial seed: %v", err)
	}
	accounts := appPolicyDemoAccounts(t, q)
	provider, err := q.GetUpstreamIDPBySlugAny(ctx, policyDemoUpstreamIDPSlug)
	if err != nil {
		t.Fatalf("load policy demo provider: %v", err)
	}
	providerID := provider.ID
	if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
		AccountID:   accounts["alice"].ID,
		Source:      "user",
		Bytes:       []byte("alice-existing-avatar"),
		ContentType: pgtype.Text{String: "image/custom-alice", Valid: true},
		Etag:        pgtype.Text{String: "alice-existing-etag", Valid: true},
	}); err != nil {
		t.Fatalf("replace alice avatar fixture: %v", err)
	}
	if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
		AccountID:   accounts["bob"].ID,
		Source:      "upstream:" + policyDemoUpstreamIDPSlug,
		Bytes:       []byte("bob-existing-avatar"),
		ContentType: pgtype.Text{String: "image/custom-bob", Valid: true},
		Etag:        pgtype.Text{String: "bob-existing-etag", Valid: true},
		IdpID:       &providerID,
	}); err != nil {
		t.Fatalf("replace bob avatar fixture: %v", err)
	}
	if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
		AccountID:   accounts["alice"].ID,
		Source:      "alternate:alice",
		Bytes:       []byte("alice-alternate-avatar"),
		ContentType: pgtype.Text{String: "image/alternate-alice", Valid: true},
		Etag:        pgtype.Text{String: "alice-alternate-etag", Valid: true},
	}); err != nil {
		t.Fatalf("insert alice alternate avatar: %v", err)
	}
	if err := q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
		AccountID:   accounts["bob"].ID,
		Source:      "alternate:bob",
		Bytes:       []byte("bob-alternate-avatar"),
		ContentType: pgtype.Text{String: "image/alternate-bob", Valid: true},
		Etag:        pgtype.Text{String: "bob-alternate-etag", Valid: true},
	}); err != nil {
		t.Fatalf("insert bob alternate avatar: %v", err)
	}
	if err := q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{AccountID: accounts["alice"].ID, Source: "alternate:alice"}); err != nil {
		t.Fatalf("select alice alternate avatar: %v", err)
	}
	if err := q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{AccountID: accounts["bob"].ID, Source: "alternate:bob"}); err != nil {
		t.Fatalf("select bob alternate avatar: %v", err)
	}

	aliceBefore := appPolicyDemoAvatarSourceState(t, ctx, pool, accounts["alice"].ID, "user")
	bobBefore := appPolicyDemoAvatarSourceState(t, ctx, pool, accounts["bob"].ID, "upstream:"+policyDemoUpstreamIDPSlug)
	aliceActiveBefore := appPolicyDemoActiveAvatarStateFor(t, ctx, q, accounts["alice"].ID)
	bobActiveBefore := appPolicyDemoActiveAvatarStateFor(t, ctx, q, accounts["bob"].ID)

	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err != nil {
		t.Fatalf("rerun seed: %v", err)
	}

	if got := appPolicyDemoAvatarSourceState(t, ctx, pool, accounts["alice"].ID, "user"); !reflect.DeepEqual(got, aliceBefore) {
		t.Errorf("alice avatar source after rerun = %#v, want %#v", got, aliceBefore)
	}
	if got := appPolicyDemoAvatarSourceState(t, ctx, pool, accounts["bob"].ID, "upstream:"+policyDemoUpstreamIDPSlug); !reflect.DeepEqual(got, bobBefore) {
		t.Errorf("bob avatar source after rerun = %#v, want %#v", got, bobBefore)
	}
	if got := appPolicyDemoActiveAvatarStateFor(t, ctx, q, accounts["alice"].ID); !reflect.DeepEqual(got, aliceActiveBefore) {
		t.Errorf("alice active avatar after rerun = %#v, want %#v", got, aliceActiveBefore)
	}
	if got := appPolicyDemoActiveAvatarStateFor(t, ctx, q, accounts["bob"].ID); !reflect.DeepEqual(got, bobActiveBefore) {
		t.Errorf("bob active avatar after rerun = %#v, want %#v", got, bobActiveBefore)
	}
}

func TestSeedAppPolicyDemoIsIdempotentAndPreservesUnrelatedData(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := appPolicyDemoTestPool(t)
	t.Cleanup(cleanup)
	q := db.New(pool)
	accounts := appPolicyDemoAccounts(t, q)
	custom, err := q.CreateOIDCAppGroup(ctx, db.CreateOIDCAppGroupParams{
		Kind:                "rule",
		Slug:                "custom-local",
		DisplayName:         "Custom local rule",
		Description:         pgtype.Text{String: "Unrelated data", Valid: true},
		ExposedToDownstream: true,
		Rule:                []byte(`{"version":1,"condition":{"fact":"avatar","source":"any"}}`),
		OidcClientID:        appPolicyDemoClientID,
	})
	if err != nil {
		t.Fatalf("create unrelated group: %v", err)
	}
	if err := q.AssignOIDCClientManager(ctx, db.AssignOIDCClientManagerParams{ClientID: appPolicyDemoClientID, AccountID: accounts["bob"].ID}); err != nil {
		t.Fatalf("assign unrelated manager: %v", err)
	}
	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := appPolicyDemoFixtureCounts(t, pool)
	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second := appPolicyDemoFixtureCounts(t, pool)
	if first != second {
		t.Fatalf("fixture counts changed on rerun: first=%+v second=%+v", first, second)
	}
	groups := appPolicyDemoGroupMap(t, q)
	if got, ok := groups["custom-local"]; !ok || got.ID != custom.ID || got.DisplayName != custom.DisplayName {
		t.Fatalf("unrelated group changed: %+v, want %+v", got, custom)
	}
	isManager, err := q.IsOIDCClientManager(ctx, db.IsOIDCClientManagerParams{ClientID: appPolicyDemoClientID, AccountID: accounts["bob"].ID})
	if err != nil || !isManager {
		t.Fatalf("unrelated manager preserved = %v, err = %v", isManager, err)
	}
}

type appPolicyDemoCounts struct {
	credentials int
	passwords   int
	totps       int
	identities  int
	groups      int
	managers    int
	decisions   int
}

func appPolicyDemoFixtureCounts(t *testing.T, pool *pgxpool.Pool) appPolicyDemoCounts {
	t.Helper()
	ctx := context.Background()
	var counts appPolicyDemoCounts
	for _, query := range []struct {
		name string
		dest *int
		SQL  string
	}{
		{"credentials", &counts.credentials, `SELECT count(*) FROM webauthn_credential w JOIN account a ON a.id = w.account_id WHERE a.username = 'alice'`},
		{"passwords", &counts.passwords, `SELECT count(*) FROM password_credential p JOIN account a ON a.id = p.account_id WHERE a.username = 'bob'`},
		{"totps", &counts.totps, `SELECT count(*) FROM totp_credential t JOIN account a ON a.id = t.account_id WHERE a.username = 'bob'`},
		{"identities", &counts.identities, `SELECT count(*) FROM account_identity ai JOIN account a ON a.id = ai.account_id JOIN upstream_idp i ON i.id = ai.upstream_idp_id WHERE a.username = 'alice' AND i.slug = 'downstream-policy-demo'`},
		{"groups", &counts.groups, `SELECT count(*) FROM user_group WHERE oidc_client_id = $1 AND slug LIKE 'demo-%'`},
		{"managers", &counts.managers, `SELECT count(*) FROM oidc_client_manager WHERE client_id = $1`},
		{"decisions", &counts.decisions, `SELECT count(*) FROM group_manual_decision d JOIN user_group g ON g.id = d.group_id WHERE g.oidc_client_id = $1 AND g.slug = $2`},
	} {
		args := []any{}
		if strings.Contains(query.SQL, "$1") {
			args = append(args, appPolicyDemoClientID)
		}
		if strings.Contains(query.SQL, "$2") {
			args = append(args, appPolicyDemoManualSlug)
		}
		if err := pool.QueryRow(ctx, query.SQL, args...).Scan(query.dest); err != nil {
			t.Fatalf("count %s: %v", query.name, err)
		}
	}
	return counts
}

func TestSeedAppPolicyDemoWrongKindConflictRollsBack(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := appPolicyDemoTestPool(t)
	t.Cleanup(cleanup)
	q := db.New(pool)
	if _, err := q.CreateOIDCAppGroup(ctx, db.CreateOIDCAppGroupParams{
		Kind:         "manual",
		Slug:         "demo-passkey-login",
		DisplayName:  "Wrong kind",
		OidcClientID: appPolicyDemoClientID,
	}); err != nil {
		t.Fatalf("create conflicting group: %v", err)
	}
	if err := seedAppPolicyDemo(ctx, pool, appPolicyDemoConfig()); err == nil || !strings.Contains(err.Error(), "wrong kind") || !strings.Contains(err.Error(), "demo-passkey-login") {
		t.Fatalf("seed error = %v, want wrong kind conflict for demo-passkey-login", err)
	}

	accounts := appPolicyDemoAccounts(t, q)
	if accounts["alice"].Role != "user" {
		t.Fatalf("alice role after rollback = %q, want user", accounts["alice"].Role)
	}
	client, err := q.GetOIDCClient(ctx, appPolicyDemoClientID)
	if err != nil {
		t.Fatal(err)
	}
	if client.AccessRestricted {
		t.Fatal("dev-app access restriction committed despite rollback")
	}
	if managers, err := q.ListOIDCClientManagers(ctx, appPolicyDemoClientID); err != nil || len(managers) != 0 {
		t.Fatalf("manager assignments after rollback = %+v, err = %v", managers, err)
	}
	groups := appPolicyDemoGroupMap(t, q)
	if len(groups) != 1 || groups["demo-passkey-login"].Kind != "manual" {
		t.Fatalf("groups after rollback = %+v, want only conflict", groups)
	}
	for _, username := range []string{"alice", "bob"} {
		facts, err := q.GetAccountAccessFacts(ctx, accounts[username].ID)
		if err != nil {
			t.Fatalf("get facts for %s: %v", username, err)
		}
		if facts.HasPasskey || facts.HasPasswordTotp || facts.HasFederation || facts.HasAnyAvatar || facts.HasUserAvatar || len(facts.ConfirmedProviderSlugs) != 0 || len(facts.ConfirmedProtocols) != 0 {
			t.Errorf("facts for %s committed despite rollback: %+v", username, facts)
		}
	}
}
