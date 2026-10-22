package configx

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// resetViper isolates each test from the package-global viper state and the
// env vars Parse() reads. The DATA_ENCRYPTION_KEY var is required by Parse so
// every subtest sets it before running.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	// Strip any inherited prohibitorum env so the test owns the surface.
	for _, kv := range os.Environ() {
		for _, prefix := range []string{"PROHIBITORUM_"} {
			if len(kv) >= len(prefix) && kv[:len(prefix)] == prefix {
				// kv looks like KEY=VALUE
				eq := -1
				for i := 0; i < len(kv); i++ {
					if kv[i] == '=' {
						eq = i
						break
					}
				}
				if eq > 0 {
					t.Setenv(kv[:eq], "")
				}
			}
		}
	}
	// Provide a key so Parse doesn't reject the config outright.
	t.Setenv("PROHIBITORUM_DATA_ENCRYPTION_KEY_V1", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestParse_AuthDefaults(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantSchedule := []time.Duration{
		0, 0,
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second,
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		15 * time.Minute,
	}
	if len(cfg.Auth.ThrottleSchedule) != len(wantSchedule) {
		t.Fatalf("ThrottleSchedule length: want %d, got %d (%v)",
			len(wantSchedule), len(cfg.Auth.ThrottleSchedule), cfg.Auth.ThrottleSchedule)
	}
	for i, w := range wantSchedule {
		if cfg.Auth.ThrottleSchedule[i] != w {
			t.Errorf("ThrottleSchedule[%d]: want %v, got %v", i, w, cfg.Auth.ThrottleSchedule[i])
		}
	}

	if cfg.Auth.SudoTTL != 30*time.Minute {
		t.Errorf("Auth.SudoTTL: want 30m, got %v", cfg.Auth.SudoTTL)
	}
	if cfg.Auth.PartialSessionTTL != 5*time.Minute {
		t.Errorf("Auth.PartialSessionTTL: want 5m, got %v", cfg.Auth.PartialSessionTTL)
	}
}

func TestParse_PasswordHashParallelismDefault(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.PasswordHashParams.Parallelism != 1 {
		t.Errorf("PasswordHashParams.Parallelism default: want 1, got %d", cfg.PasswordHashParams.Parallelism)
	}
	// Memory/iteration defaults must not regress.
	if cfg.PasswordHashParams.MemoryKiB != 65536 {
		t.Errorf("PasswordHashParams.MemoryKiB: want 65536, got %d", cfg.PasswordHashParams.MemoryKiB)
	}
	if cfg.PasswordHashParams.Iterations != 3 {
		t.Errorf("PasswordHashParams.Iterations: want 3, got %d", cfg.PasswordHashParams.Iterations)
	}
}

func TestParse_TOTPIssuerFallsBackToRPDisplayName(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// totp.issuer is left blank by default → must resolve to the (also
	// defaulted) webauthn.rp_display_name = "Prohibitorum".
	if cfg.TOTP.Issuer != "Prohibitorum" {
		t.Errorf("TOTP.Issuer fallback: want %q, got %q", "Prohibitorum", cfg.TOTP.Issuer)
	}
}

func TestParse_TOTPIssuerExplicitOverride(t *testing.T) {
	resetViper(t)
	t.Setenv("PROHIBITORUM_TOTP_ISSUER", "Acme Corp")
	t.Setenv("PROHIBITORUM_WEBAUTHN_RP_DISPLAY_NAME", "Acme Auth")
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TOTP.Issuer != "Acme Corp" {
		t.Errorf("explicit totp.issuer must win over rp_display_name fallback: got %q", cfg.TOTP.Issuer)
	}
}

// TestParse_SessionRememberTTL covers the "keep me signed in" session
// lifetime: 14d default, PROHIBITORUM_SESSION_REMEMBER_TTL override, and the
// ≤0 fallback that restores the default instead of disabling remembered
// sessions.
func TestParse_SessionRememberTTL(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SessionRememberTTL != 14*24*time.Hour {
		t.Errorf("SessionRememberTTL default: want %v, got %v", 14*24*time.Hour, cfg.SessionRememberTTL)
	}

	resetViper(t)
	t.Setenv("PROHIBITORUM_SESSION_REMEMBER_TTL", "720h")
	cfg, err = Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SessionRememberTTL != 720*time.Hour {
		t.Errorf("SessionRememberTTL override: want 720h, got %v", cfg.SessionRememberTTL)
	}

	resetViper(t)
	t.Setenv("PROHIBITORUM_SESSION_REMEMBER_TTL", "-1h")
	cfg, err = Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SessionRememberTTL != 14*24*time.Hour {
		t.Errorf("SessionRememberTTL ≤0 must fall back to default: want %v, got %v", 14*24*time.Hour, cfg.SessionRememberTTL)
	}
}

// TestParse_RejectsShortDEK asserts that a base64-valid but cryptographically
// undersized data-encryption key is rejected at Parse startup. A 4-byte key
// decodes cleanly from base64 but is far too short for AES-256, so the failure
// must surface now, not at first use. The error must name the version and the
// actual length so an operator can identify which env var is misconfigured.
func TestParse_RejectsShortDEK(t *testing.T) {
	resetViper(t)
	// Override the valid key injected by resetViper with a 4-byte (base64-valid) key.
	t.Setenv("PROHIBITORUM_DATA_ENCRYPTION_KEY_V1", base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}))
	_, err := Parse()
	if err == nil {
		t.Fatal("Parse accepted a 4-byte DEK; want a length-validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "v1") {
		t.Errorf("error %q does not name the key version (v1)", msg)
	}
	if !strings.Contains(msg, "4") {
		t.Errorf("error %q does not name the actual byte length (4)", msg)
	}
	// The error must never include key material — only version + length.
	if strings.Contains(msg, base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})) {
		t.Errorf("error %q leaks the raw key material", msg)
	}
}

// TestParse_AcceptsValidDEKLengths confirms a 32-byte key (the valid AES-256
// DEK length) still passes Parse after length validation is enforced, and a
// two-version keyring of valid keys is accepted.
func TestParse_AcceptsValidDEKLengths(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse with valid 32-byte DEK: %v", err)
	}
	if len(cfg.DataEncryptionKeys) != 1 {
		t.Errorf("DataEncryptionKeys: want 1, got %d", len(cfg.DataEncryptionKeys))
	}

	resetViper(t)
	t.Setenv("PROHIBITORUM_DATA_ENCRYPTION_KEY_V2", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	cfg, err = Parse()
	if err != nil {
		t.Fatalf("Parse with two valid 32-byte DEKs: %v", err)
	}
	if len(cfg.DataEncryptionKeys) != 2 {
		t.Errorf("DataEncryptionKeys: want 2, got %d", len(cfg.DataEncryptionKeys))
	}
}

// TestParse_FederationHasNoGlobalAllowPrivateNetwork verifies that the global
// federation.allow_private_network config bypass has been removed in favor of
// the per-IdP database column (migration 030). The FederationConfig struct
// must not carry the field, and Parse must not register a default for it.
func TestParse_FederationHasNoGlobalAllowPrivateNetwork(t *testing.T) {
	resetViper(t)

	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// FederationConfig must not carry AllowPrivateNetwork — the field is
	// gone (per-IdP DB column replaces it). StateTTL and DefaultScopes
	// remain.
	_ = cfg.Federation.StateTTL
	_ = cfg.Federation.DefaultScopes

	// Viper must not carry a registered default for the removed key.
	// (AutomaticEnv means an explicit env var can still make IsSet return
	// true, but with no env var set and no default registered, the key
	// should be absent.)
	if viper.IsSet("federation.allow_private_network") {
		t.Fatal("federation.allow_private_network is still registered as a config key — global bypass should be removed")
	}
}

// TestParse_RefreshTTLDefaults verifies the inactivity/absolute refresh-token
// lifetime defaults: 30d inactivity / 90d absolute.
func TestParse_RefreshTTLDefaults(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.OIDC.RefreshTokenInactivityTTL != 30*24*time.Hour {
		t.Errorf("RefreshTokenInactivityTTL default: want 30d, got %v", cfg.OIDC.RefreshTokenInactivityTTL)
	}
	if cfg.OIDC.RefreshTokenAbsoluteTTL != 90*24*time.Hour {
		t.Errorf("RefreshTokenAbsoluteTTL default: want 90d, got %v", cfg.OIDC.RefreshTokenAbsoluteTTL)
	}
}

// TestParse_RefreshTTLValidation rejects non-positive values and an absolute
// lifetime shorter than the inactivity window.
func TestParse_RefreshTTLValidation(t *testing.T) {
	t.Run("non_positive_inactivity", func(t *testing.T) {
		resetViper(t)
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_INACTIVITY_TTL", "0")
		_, err := Parse()
		if err == nil {
			t.Fatal("Parse accepted zero inactivity TTL")
		}
	})
	t.Run("non_positive_absolute", func(t *testing.T) {
		resetViper(t)
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_ABSOLUTE_TTL", "0")
		_, err := Parse()
		if err == nil {
			t.Fatal("Parse accepted zero absolute TTL")
		}
	})
	t.Run("absolute_shorter_than_inactivity", func(t *testing.T) {
		resetViper(t)
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_INACTIVITY_TTL", "48h")
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_ABSOLUTE_TTL", "24h")
		_, err := Parse()
		if err == nil {
			t.Fatal("Parse accepted absolute TTL shorter than inactivity TTL")
		}
	})
	t.Run("absolute_equal_to_inactivity", func(t *testing.T) {
		resetViper(t)
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_INACTIVITY_TTL", "48h")
		t.Setenv("PROHIBITORUM_OIDC_REFRESH_TOKEN_ABSOLUTE_TTL", "48h")
		cfg, err := Parse()
		if err != nil {
			t.Fatalf("Parse rejected absolute == inactivity: %v", err)
		}
		if cfg.OIDC.RefreshTokenAbsoluteTTL != 48*time.Hour {
			t.Errorf("absolute TTL: want 48h, got %v", cfg.OIDC.RefreshTokenAbsoluteTTL)
		}
	})
}

func TestParse_PublicOriginValidation(t *testing.T) {
	for _, tc := range []struct {
		value   string
		wantErr string
	}{
		{value: "https://id.example.com"},
		{value: "https://id.example.com:8443, http://localhost:8080"},
		{value: "https://id.example.com/", wantErr: `must not end with "/"; use "https://id.example.com"`},
		{value: "https://id.example.com/idp", wantErr: "with no path, query or fragment"},
		{value: "https://id.example.com?x=1", wantErr: "with no path, query or fragment"},
		{value: "https://id.example.com#top", wantErr: "with no path, query or fragment"},
		{value: "id.example.com", wantErr: "must be scheme://host[:port]"},
		{value: "ftp://id.example.com", wantErr: "must be scheme://host[:port]"},
		{value: "https://user@id.example.com", wantErr: "must be scheme://host[:port]"},
		{value: "https://id.example.com:", wantErr: "must be scheme://host[:port]"},
		{value: "https://id.example.com#", wantErr: "with no path, query or fragment"},
		{value: "https://id.example.com?", wantErr: "with no path, query or fragment"},
		{value: "https://id.example.com/#", wantErr: "with no path, query or fragment"},
		{value: "https://id.example.com, https://other.example.com/", wantErr: `entry "https://other.example.com/" must not end with "/"`},
	} {
		t.Run(tc.value, func(t *testing.T) {
			resetViper(t)
			t.Setenv("PROHIBITORUM_PUBLIC_ORIGIN", tc.value)
			_, err := Parse()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "PROHIBITORUM_PUBLIC_ORIGIN") {
				t.Fatalf("Parse() error = %v, want one naming PROHIBITORUM_PUBLIC_ORIGIN and containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestParse_SMTPDefaults checks the disabled-by-default shape: empty host,
// but valid port/security/timeout defaults for when an operator fills in the
// host alone.
func TestParse_SMTPDefaults(t *testing.T) {
	resetViper(t)
	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SMTP.Host != "" || cfg.SMTP.From != "" {
		t.Errorf("SMTP host/from should default empty, got %q/%q", cfg.SMTP.Host, cfg.SMTP.From)
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP port default: want 587, got %d", cfg.SMTP.Port)
	}
	if cfg.SMTP.Security != "starttls" {
		t.Errorf("SMTP security default: want starttls, got %q", cfg.SMTP.Security)
	}
	if cfg.SMTP.Timeout != 15*time.Second {
		t.Errorf("SMTP timeout default: want 15s, got %v", cfg.SMTP.Timeout)
	}
}

// TestParse_SMTPValidation covers the three refusal cases (missing/bad from,
// bad security) and the valid shape.
func TestParse_SMTPValidation(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"host_without_from", map[string]string{
			"PROHIBITORUM_SMTP_HOST": "smtp.example.com",
		}, "smtp.from is required"},
		{"from_with_display_name", map[string]string{
			"PROHIBITORUM_SMTP_HOST": "smtp.example.com",
			"PROHIBITORUM_SMTP_FROM": "Prohibitorum <noreply@example.com>",
		}, "bare email address"},
		{"bad_security", map[string]string{
			"PROHIBITORUM_SMTP_HOST":     "smtp.example.com",
			"PROHIBITORUM_SMTP_FROM":     "noreply@example.com",
			"PROHIBITORUM_SMTP_SECURITY": "ssl",
		}, "smtp.security"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetViper(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Parse()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	t.Run("valid_config_passes", func(t *testing.T) {
		resetViper(t)
		t.Setenv("PROHIBITORUM_SMTP_HOST", "smtp.example.com")
		t.Setenv("PROHIBITORUM_SMTP_FROM", "noreply@example.com")
		cfg, err := Parse()
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if cfg.SMTP.Host != "smtp.example.com" || cfg.SMTP.From != "noreply@example.com" {
			t.Errorf("SMTP roundtrip: %+v", cfg.SMTP)
		}
	})
}
