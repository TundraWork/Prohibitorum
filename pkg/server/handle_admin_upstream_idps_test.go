package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	federationsteam "prohibitorum/pkg/federation/providers/steam"
	federationvrchat "prohibitorum/pkg/federation/providers/vrchat"
)

const exactOIDCConfig = `{
  "issuerUrl":"https://issuer.example",
  "clientId":"client-id",
  "scopes":["openid","profile","email"],
  "allowedDomains":[],
  "usernameClaim":"preferred_username",
  "displayNameClaim":"name",
  "emailClaim":"email",
  "pictureClaim":"picture",
  "requireVerifiedEmail":true,
  "allowPrivateNetwork":false
}`

func newProviderAdminTestServer(t *testing.T) *Server {
	t.Helper()
	registry := federation.NewRegistry()
	for _, definition := range []federation.Definition{
		federationoidc.Definition{}, federationsteam.Definition{}, federationvrchat.Definition{},
	} {
		if err := registry.RegisterDefinition(definition); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{
		config:             &configx.Config{DataEncryptionKeys: map[int][]byte{1: make([]byte, 32)}},
		federationRegistry: registry,
	}
}

func readyOIDCRow(status string) db.UpstreamIdp {
	return db.UpstreamIdp{
		ID:             42,
		Slug:           "corp",
		DisplayName:    "Corporate",
		Protocol:       federationoidc.Protocol,
		Mode:           federation.ModeAutoProvision,
		ProviderConfig: []byte(exactOIDCConfig),
		SecretEnc:      []byte("ciphertext-must-not-leak"),
		SecretNonce:    []byte("nonce-must-not-leak"),
		KeyVersion:     pgtype.Int4{Int32: 1, Valid: true},
		SecretStatus:   status,
		Disabled:       true,
		CreatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC), Valid: true},
	}
}

func TestIdentityProviderViewUsesGenericConfigHealthAndDescriptor(t *testing.T) {
	t.Parallel()
	s := newProviderAdminTestServer(t)
	validatedAt := time.Date(2026, 7, 16, 2, 3, 4, 0, time.UTC)
	row := readyOIDCRow("valid")
	row.SecretValidatedAt = pgtype.Timestamptz{Time: validatedAt, Valid: true}

	view, err := s.identityProviderView(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(view.Config) != exactOIDCConfig {
		t.Fatalf("Config = %s, want raw validated config", view.Config)
	}
	if !view.SecretConfigured || view.SecretStatus != "valid" || view.SecretValidatedAt == nil || !view.SecretValidatedAt.Equal(validatedAt) {
		t.Fatalf("secret health = configured:%v status:%q validated:%v", view.SecretConfigured, view.SecretStatus, view.SecretValidatedAt)
	}
	if !view.Ready {
		t.Fatal("Ready = false for valid sealed provider")
	}
	if len(view.SearchFields) != 2 || view.SearchFields[0].Key != "subject" || len(view.SearchFields[1].Operators) != 3 || view.SearchFields[1].Operators[2] != "contains" {
		t.Fatalf("SearchFields = %#v", view.SearchFields)
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ciphertext-must-not-leak", "nonce-must-not-leak", "secretEnc", "secretNonce", "keyVersion"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestIdentityProviderViewUnconfiguredSecretIsNullable(t *testing.T) {
	t.Parallel()
	s := newProviderAdminTestServer(t)
	view, err := s.identityProviderView(db.UpstreamIdp{
		Slug: "vrchat", DisplayName: "VRChat", Protocol: federationvrchat.Protocol,
		Mode: federation.ModeLinkOnly, ProviderConfig: []byte(`{}`), SecretStatus: "unconfigured", Disabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.SecretConfigured || view.SecretValidatedAt != nil || view.Ready {
		t.Fatalf("unconfigured view = %#v", view)
	}
	if !view.SupportsOperator || len(view.SearchFields) != 2 || view.SearchFields[0].Key != "userId" {
		t.Fatalf("VRChat descriptor = operator:%v fields:%#v", view.SupportsOperator, view.SearchFields)
	}
}

func TestNormalizeProviderSlugUsesDatabaseCutset(t *testing.T) {
	t.Parallel()
	if got := normalizeProviderSlug("\tVRChat-Main\r\n"); got != "vrchat-main" {
		t.Fatalf("ASCII whitespace normalization = %q, want vrchat-main", got)
	}
	if got := normalizeProviderSlug("\u2003VRChat\u2003"); got != "\u2003vrchat\u2003" {
		t.Fatalf("non-ASCII normalization = %q, want preserved edge characters", got)
	}
}

func TestValidateProviderWrite(t *testing.T) {
	t.Parallel()
	s := newProviderAdminTestServer(t)
	validOIDC := providerWriteBody{
		Slug: "corp", DisplayName: "Corporate", Protocol: "oidc", Mode: federation.ModeAutoProvision,
		Config: json.RawMessage(exactOIDCConfig), Secret: "client-secret",
	}
	for _, tc := range []struct {
		name    string
		body    providerWriteBody
		wantErr bool
	}{
		{name: "valid OIDC", body: validOIDC},
		{name: "invalid OIDC config", body: providerWriteBody{Slug: "corp", DisplayName: "Corporate", Protocol: "oidc", Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{"issuerUrl":"http://127.0.0.1"}`), Secret: "secret"}, wantErr: true},
		{name: "Steam exact empty config", body: providerWriteBody{Slug: "steam", DisplayName: "Steam", Protocol: "steam", Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{}`), Secret: "api-key"}},
		{name: "Steam rejects non-empty config", body: providerWriteBody{Slug: "steam", DisplayName: "Steam", Protocol: "steam", Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{"unexpected":true}`), Secret: "api-key"}, wantErr: true},
		{name: "VRChat exact empty config", body: providerWriteBody{Slug: "vrchat", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{}`)}},
		{name: "VRChat rejects non-empty config", body: providerWriteBody{Slug: "vrchat", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{"unexpected":true}`)}, wantErr: true},
		{name: "unknown protocol", body: providerWriteBody{Slug: "unknown", DisplayName: "Unknown", Protocol: "oauth1", Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{}`), Secret: "secret"}, wantErr: true},
		{name: "oversize config", body: providerWriteBody{Slug: "vrchat", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{"padding":"` + strings.Repeat("x", 8192) + `"}`)}, wantErr: true},
		{name: "OIDC missing secret", body: providerWriteBody{Slug: "corp", DisplayName: "Corporate", Protocol: "oidc", Mode: federation.ModeAutoProvision, Config: json.RawMessage(exactOIDCConfig)}, wantErr: true},
		{name: "Steam missing secret", body: providerWriteBody{Slug: "steam", DisplayName: "Steam", Protocol: "steam", Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{}`)}, wantErr: true},
		{name: "VRChat rejects generic secret", body: providerWriteBody{Slug: "vrchat", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{}`), Secret: "not-allowed"}, wantErr: true},
		{name: "mixed-case slug", body: providerWriteBody{Slug: "VRChat-Main", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{}`)}, wantErr: true},
		{name: "slug with surrounding whitespace", body: providerWriteBody{Slug: " vrchat ", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{}`)}, wantErr: true},
		{name: "missing common field", body: providerWriteBody{Slug: "corp", Protocol: "oidc", Mode: federation.ModeAutoProvision, Config: json.RawMessage(exactOIDCConfig), Secret: "secret"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.validateProviderWrite(tc.body, nil, true)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateProviderWrite() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateProviderUpdateEnforcesImmutableBindingAndDedicatedSecretRoute(t *testing.T) {
	t.Parallel()
	s := newProviderAdminTestServer(t)
	existing := readyOIDCRow("configured")
	valid := providerWriteBody{DisplayName: "Renamed", Mode: federation.ModeInviteOnly, Config: json.RawMessage(exactOIDCConfig)}
	if _, err := s.validateProviderWrite(valid, &existing, false); err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	for _, body := range []providerWriteBody{
		{Slug: "other", DisplayName: valid.DisplayName, Mode: valid.Mode, Config: valid.Config},
		{Protocol: "steam", DisplayName: valid.DisplayName, Mode: valid.Mode, Config: valid.Config},
		{DisplayName: valid.DisplayName, Mode: valid.Mode, Config: valid.Config, Secret: "rotate-here"},
	} {
		if _, err := s.validateProviderWrite(body, &existing, false); err == nil {
			t.Fatalf("immutable/secret update accepted: %#v", body)
		}
	}
}

func TestVRChatCreateDefaultsUseNullSecretTuple(t *testing.T) {
	t.Parallel()
	params := providerInsertParams(providerWriteBody{
		Slug: "vrchat", DisplayName: "VRChat", Protocol: "vrchat", Mode: federation.ModeLinkOnly, Config: json.RawMessage(`{}`),
	})
	if !params.Disabled || params.SecretStatus != "unconfigured" {
		t.Fatalf("VRChat defaults = disabled:%v status:%q", params.Disabled, params.SecretStatus)
	}
	if params.SecretEnc != nil || params.SecretNonce != nil || params.KeyVersion.Valid {
		t.Fatalf("VRChat secret tuple = (%v, %v, %#v), want all null", params.SecretEnc, params.SecretNonce, params.KeyVersion)
	}
}

func TestProviderWriteAuditDetailExcludesSecret(t *testing.T) {
	t.Parallel()
	detail := providerWriteAuditDetail(providerWriteBody{
		Slug: "corp", DisplayName: "Corporate", Protocol: "oidc", Mode: federation.ModeAutoProvision,
		Config: json.RawMessage(exactOIDCConfig), Secret: "plaintext-must-not-leak",
	})
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "plaintext-must-not-leak") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("audit detail leaked secret: %s", encoded)
	}
}

type fakeProviderStateQueries struct {
	row      db.UpstreamIdp
	setCalls int
}

func (f *fakeProviderStateQueries) GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error) {
	return f.row, nil
}

func (f *fakeProviderStateQueries) SetUpstreamIDPDisabled(_ context.Context, arg db.SetUpstreamIDPDisabledParams) (db.UpstreamIdp, error) {
	f.setCalls++
	f.row.Disabled = arg.Disabled
	return f.row, nil
}

func TestSetIdentityProviderDisabledReadinessContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     string
		disabled   bool
		wantStatus int
		wantCode   string
		wantSets   int
	}{
		{name: "disabling is always allowed", status: "configured", disabled: true, wantStatus: http.StatusOK, wantSets: 1},
		{name: "unready enable is rejected", status: "invalid", disabled: false, wantStatus: http.StatusServiceUnavailable, wantCode: "provider_not_ready"},
		{name: "configured provider enable succeeds", status: "configured", disabled: false, wantStatus: http.StatusOK, wantSets: 1},
		{name: "ready enable succeeds", status: "valid", disabled: false, wantStatus: http.StatusOK, wantSets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newProviderAdminTestServer(t)
			row := readyOIDCRow(tc.status)
			row.Disabled = true
			queries := &fakeProviderStateQueries{row: row}
			s.providerStateQueriesOverride = queries
			req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/identity-providers/set-disabled", strings.NewReader(`{"slug":"corp","disabled":`+jsonBool(tc.disabled)+`}`))
			w := httptest.NewRecorder()

			s.handleSetIdentityProviderDisabledHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			if queries.setCalls != tc.wantSets {
				t.Fatalf("SetUpstreamIDPDisabled calls = %d, want %d", queries.setCalls, tc.wantSets)
			}
			if tc.wantCode != "" {
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Code != tc.wantCode {
					t.Fatalf("code = %q, want %q", body.Code, tc.wantCode)
				}
			}
		})
	}
}

func TestVRChatProviderEnableRequiresValidOperatorSession(t *testing.T) {
	for _, test := range []struct {
		name       string
		row        db.UpstreamIdp
		wantStatus int
		wantSets   int
	}{
		{name: "unconfigured rejected", row: db.UpstreamIdp{ID: 7, Slug: "social", Protocol: "vrchat", ProviderConfig: []byte(`{}`), SecretStatus: "unconfigured", Disabled: true}, wantStatus: http.StatusServiceUnavailable},
		{name: "invalid rejected", row: db.UpstreamIdp{ID: 7, Slug: "social", Protocol: "vrchat", ProviderConfig: []byte(`{}`), SecretEnc: []byte{1}, SecretNonce: []byte{2}, KeyVersion: pgtype.Int4{Int32: 3, Valid: true}, SecretStatus: "invalid", Disabled: true}, wantStatus: http.StatusServiceUnavailable},
		{name: "valid enabled", row: db.UpstreamIdp{ID: 7, Slug: "social", Protocol: "vrchat", ProviderConfig: []byte(`{}`), SecretEnc: []byte{1}, SecretNonce: []byte{2}, KeyVersion: pgtype.Int4{Int32: 3, Valid: true}, SecretStatus: "valid", Disabled: true}, wantStatus: http.StatusOK, wantSets: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newProviderAdminTestServer(t)
			queries := &fakeProviderStateQueries{row: test.row}
			s.providerStateQueriesOverride = queries
			req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/identity-providers/set-disabled", strings.NewReader(`{"slug":"social","disabled":false}`))
			w := httptest.NewRecorder()
			s.handleSetIdentityProviderDisabledHTTP(w, req)
			if w.Code != test.wantStatus || queries.setCalls != test.wantSets {
				t.Fatalf("status=%d body=%s set_calls=%d", w.Code, w.Body.String(), queries.setCalls)
			}
		})
	}
}

func jsonBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
