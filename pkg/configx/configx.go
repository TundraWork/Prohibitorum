package configx

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is parsed from env vars (PROHIBITORUM_* prefix) and an optional
// config.yaml in the working directory.
type Config struct {
	DatabaseURL string `mapstructure:"database_url"`
	// Host is the interface the HTTP server binds to. Empty (the default) binds
	// all interfaces (":<port>"); set e.g. "127.0.0.1" to listen loopback-only
	// behind a reverse proxy.
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`

	PublicOrigins []string      `mapstructure:"public_origin"`
	SessionTTL    time.Duration `mapstructure:"session_ttl"`
	TrustProxy    bool          `mapstructure:"trust_proxy"`

	KV                 KVConfig           `mapstructure:"kv"`
	OIDC               OIDCConfig         `mapstructure:"oidc"`
	WebAuthn           WebAuthnConfig     `mapstructure:"webauthn"`
	Federation         FederationConfig   `mapstructure:"federation"`
	TOTP               TOTPConfig         `mapstructure:"totp"`
	SAML               SAMLConfig         `mapstructure:"saml"`
	PasswordHashParams PasswordHashParams `mapstructure:"password_hash"`
	Auth               AuthConfig         `mapstructure:"auth"`
	Branding           BrandingConfig     `mapstructure:"branding"`
	ForwardAuth        ForwardAuthConfig  `mapstructure:"forward_auth"`

	// DataEncryptionKeys is the versioned AES-256 key set used to encrypt
	// sensitive credential material (TOTP secrets in v0.2, additional fields
	// in later versions). Loaded from PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>
	// env vars and keyed by version. New writes use the highest version;
	// older versions remain available for decryption.
	DataEncryptionKeys map[int][]byte `mapstructure:"-"`
}

// WebAuthnConfig holds relying-party display metadata for the WebAuthn
// ceremonies. RPID and RPOrigins are typically derived from PublicOrigins,
// but can be overridden explicitly when the RP ID differs from the origin
// hostname (e.g. shared subdomain deployments).
type WebAuthnConfig struct {
	RPID          string   `mapstructure:"rp_id"`
	RPDisplayName string   `mapstructure:"rp_display_name"`
	RPOrigins     []string `mapstructure:"rp_origins"`
}

// BrandingConfig holds the deploy-time instance identity. These are DEFAULTS;
// an admin can override the name + icon at runtime (instance_settings table).
// InstanceName is purely cosmetic — it does NOT change the WebAuthn RPDisplayName,
// the OIDC Issuer, or the TOTP issuer.
type BrandingConfig struct {
	InstanceName string `mapstructure:"instance_name"`
	// IconPath is an optional path to a PNG/JPEG/WebP the operator drops at
	// deploy. Empty = no config icon (fall back to the built-in default).
	IconPath string `mapstructure:"icon_path"`
}

type KVConfig struct {
	Driver   string `mapstructure:"driver"`
	RedisURL string `mapstructure:"redis_url"`
	// Redis AUTH + TLS. The KV store holds session lookups, single-use auth
	// codes / federation state, PKCE verifiers, and enrollment tokens, so in any
	// non-loopback deployment the Redis backend MUST be reached over an
	// authenticated, encrypted channel (audit follow-up N9). Defaults are
	// empty / false to preserve the local-dev "redis on localhost" experience.
	RedisUsername string `mapstructure:"redis_username"`
	RedisPassword string `mapstructure:"redis_password"`
	RedisTLS      bool   `mapstructure:"redis_tls"`
}

// OIDCConfig governs token lifetimes + the issuer string. The issuer
// should be the public-facing origin (e.g. https://auth.example.com)
// and is embedded in every signed token + the discovery doc.
type OIDCConfig struct {
	Issuer               string        `mapstructure:"issuer"`
	AccessTokenTTL       time.Duration `mapstructure:"access_token_ttl"`
	IDTokenTTL           time.Duration `mapstructure:"id_token_ttl"`
	RefreshTokenTTL      time.Duration `mapstructure:"refresh_token_ttl"`
	AuthorizationCodeTTL time.Duration `mapstructure:"authorization_code_ttl"`
	// JWKSCacheMaxAge sets the Cache-Control max-age on the JWKS
	// (/oauth/jwks) and discovery responses — the hint RPs use to decide how
	// long to cache the signing keys. Kept short so a signing-key rotation
	// propagates quickly; default 5m.
	JWKSCacheMaxAge time.Duration `mapstructure:"jwks_cache_max_age"`
}

// FederationConfig governs upstream-IdP federation flows.
type FederationConfig struct {
	StateTTL time.Duration `mapstructure:"state_ttl"`
	// DefaultScopes is the scope set requested from an upstream OIDC IdP when an
	// admin registers/updates one without specifying scopes (per-IdP scopes
	// override this).
	DefaultScopes []string `mapstructure:"default_scopes"`
	// AllowPrivateNetwork disables the outbound federation client's dial-time
	// SSRF screen, which otherwise refuses to connect to loopback / RFC1918 /
	// link-local (incl. cloud-metadata) / ULA addresses during OIDC discovery,
	// JWKS, and token exchange. Default false. Set true ONLY when the upstream
	// IdP legitimately lives on a trusted private/internal network — it removes
	// the protection against issuer-driven SSRF (audit follow-up N2).
	AllowPrivateNetwork bool `mapstructure:"allow_private_network"`
}

// TOTPConfig holds RFC 6238 enrollment defaults + the drift window used
// during verification. Issuer is the label embedded in the otpauth:// URI
// the authenticator displays alongside the account; when blank, it falls
// back to webauthn.rp_display_name so a single brand string suffices.
type TOTPConfig struct {
	DefaultPeriod     int    `mapstructure:"default_period"`
	DefaultDigits     int    `mapstructure:"default_digits"`
	DefaultAlgorithm  string `mapstructure:"default_algorithm"`
	DriftSteps        int    `mapstructure:"drift_steps"`
	RecoveryCodeCount int    `mapstructure:"recovery_code_count"`
	Issuer            string `mapstructure:"issuer"`
}

// AuthConfig holds cross-factor authentication tuning: the per-failure
// throttle schedule, the sudo grant window, and the partial-session token
// TTL used by the multi-step (password → TOTP) login flow.
type AuthConfig struct {
	// ThrottleSchedule is indexed by (failed_attempts - 1) and clamped to the
	// last entry once the attempt count exceeds the schedule length. A zero
	// duration means "no lockout for this attempt".
	ThrottleSchedule []time.Duration `mapstructure:"throttle_schedule"`
	// SudoTTL is the recent-auth window: how long after a full authentication
	// (or an explicit step-up) sensitive endpoints accept the session without a
	// fresh step-up.
	SudoTTL time.Duration `mapstructure:"sudo_ttl"`
	// PartialSessionTTL bounds how long a password-only session may sit
	// before it must complete the TOTP step.
	PartialSessionTTL time.Duration `mapstructure:"partial_session_ttl"`
}

// SAMLConfig holds IdP identity + the per-deployment defaults that apply
// to all registered SPs (SPs may override per-row).
type SAMLConfig struct {
	// EntityID is the IdP's SAML EntityID — the stable identifier SPs key trust
	// on. Empty (the default) derives it from PublicOrigins[0]. SAML treats the
	// EntityID as an identifier, not a location: it need not be reachable (a URN
	// is valid) and SHOULD be chosen to never change, since changing it breaks
	// every registered SP. Endpoint URLs always come from PublicOrigins[0], so an
	// operator can pin a stable EntityID independent of the HTTP origin.
	EntityID string `mapstructure:"entity_id"`
	// DefaultNameIDFormat is the NameID Format advertised in IdP metadata and
	// used as the default for newly-created SPs that don't specify one.
	DefaultNameIDFormat string `mapstructure:"default_nameid_format"`
	// SessionLifetime is the default AuthnStatement SessionNotOnOrAfter horizon
	// for SPs without a per-row session_lifetime override.
	SessionLifetime       time.Duration `mapstructure:"session_lifetime"`
	MetadataRotationGrace time.Duration `mapstructure:"metadata_rotation_grace"`
	MetadataValidity      time.Duration `mapstructure:"metadata_validity"`
}

// ForwardAuthConfig configures the native Traefik ForwardAuth provider.
// SessionTTL bounds the per-domain forward-auth cookie/session lifetime.
type ForwardAuthConfig struct {
	SessionTTL time.Duration `mapstructure:"session_ttl"`
}

// PasswordHashParams parameterises argon2id. The cryptographic invariants
// (memory ≥ 64 MiB, parallelism ≥ 1, etc.) are enforced at use time.
type PasswordHashParams struct {
	MemoryKiB   uint32 `mapstructure:"memory_kib"`
	Iterations  uint32 `mapstructure:"iterations"`
	Parallelism uint8  `mapstructure:"parallelism"`
}

func Parse() (*Config, error) {
	viper.SetEnvPrefix("PROHIBITORUM")
	viper.AutomaticEnv()

	var config Config
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		var fileLookupError viper.ConfigFileNotFoundError
		if !errors.As(err, &fileLookupError) {
			return nil, err
		}
	}

	viper.SetDefault("port", 8080)
	viper.SetDefault("kv.driver", "memory")
	viper.SetDefault("kv.redis_url", "localhost:6379")
	viper.SetDefault("kv.redis_username", "")
	viper.SetDefault("kv.redis_password", "")
	viper.SetDefault("kv.redis_tls", false)
	viper.SetDefault("public_origin", "http://localhost:8080")
	viper.SetDefault("session_ttl", 8*time.Hour)
	viper.SetDefault("trust_proxy", false)

	// OIDC defaults — short access tokens, longer refresh tokens, very
	// short authorization codes (single-use anyway).
	viper.SetDefault("oidc.issuer", "")
	viper.SetDefault("oidc.access_token_ttl", 10*time.Minute)
	viper.SetDefault("oidc.id_token_ttl", 10*time.Minute)
	viper.SetDefault("oidc.refresh_token_ttl", 720*time.Hour) // 30d
	viper.SetDefault("oidc.authorization_code_ttl", 60*time.Second)
	// 5m matches the historical hardcoded JWKS Cache-Control; kept short so a
	// signing-key rotation reaches RPs quickly.
	viper.SetDefault("oidc.jwks_cache_max_age", 5*time.Minute)

	// Federation defaults.
	viper.SetDefault("federation.state_ttl", 10*time.Minute)
	viper.SetDefault("federation.default_scopes", []string{"openid", "profile", "email"})
	// Secure by default: the outbound federation client refuses internal/metadata
	// IPs unless an operator explicitly opts in for a trusted internal IdP.
	viper.SetDefault("federation.allow_private_network", false)

	// TOTP defaults — RFC 6238 §5.2 baseline, ±1 step drift window,
	// 10 recovery codes per account. Issuer defaults to empty so the
	// post-unmarshal step can fall back to webauthn.rp_display_name.
	viper.SetDefault("totp.default_period", 30)
	viper.SetDefault("totp.default_digits", 6)
	viper.SetDefault("totp.default_algorithm", "SHA1")
	viper.SetDefault("totp.drift_steps", 1)
	viper.SetDefault("totp.recovery_code_count", 10)
	viper.SetDefault("totp.issuer", "")

	// Cross-factor auth defaults. Schedule is the canonical OWASP-style
	// exponential ladder: two free attempts, then 1s, 2s, 4s, 8s, 16s, 32s,
	// 1m, 2m, 4m, 8m, 15m — the last entry clamps for all further failures.
	// PartialSessionTTL is five minutes: short enough to bound post-compromise
	// blast radius, long enough that the user can complete a follow-up step
	// without re-authenticating. SudoTTL (the recent-auth / step-up window) is
	// longer — 15m — because it is now granted at login and is multi-use, so a
	// 5m window would re-prompt a user still actively managing settings.
	viper.SetDefault("auth.throttle_schedule", []time.Duration{
		0, 0,
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second,
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		15 * time.Minute,
	})
	viper.SetDefault("auth.sudo_ttl", 15*time.Minute)
	viper.SetDefault("auth.partial_session_ttl", 5*time.Minute)

	// SAML defaults — persistent NameID per OASIS SAML 2.0 Core §8.3.7,
	// 7d metadata rotation grace.
	viper.SetDefault("saml.entity_id", "")
	viper.SetDefault("saml.default_nameid_format", "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent")
	viper.SetDefault("saml.session_lifetime", 8*time.Hour)
	viper.SetDefault("saml.metadata_rotation_grace", 7*24*time.Hour)
	viper.SetDefault("saml.metadata_validity", 24*time.Hour)

	// Password hashing defaults — 64 MiB / 3 iterations / 1 lane. The
	// memory and time costs follow OWASP's 2024 argon2id guidance for
	// interactive auth; parallelism dropped to 1 per the 2026 update, which
	// notes that single-lane runs simplify reasoning about wall-clock cost
	// on small VPS deployments without losing meaningful brute-force
	// resistance at these memory/iteration counts.
	viper.SetDefault("password_hash.memory_kib", 65536)
	viper.SetDefault("password_hash.iterations", 3)
	viper.SetDefault("password_hash.parallelism", 1)

	// WebAuthn substruct defaults — RPDisplayName defaults to the product name;
	// RPID and RPOrigins are derived from PublicOrigins when not set explicitly.
	viper.SetDefault("webauthn.rp_display_name", "Prohibitorum")
	viper.SetDefault("webauthn.rp_id", "")

	// Branding defaults — InstanceName falls back to the built-in product name;
	// IconPath is empty (operator drops a file if desired).
	viper.SetDefault("branding.instance_name", "")
	viper.SetDefault("branding.icon_path", "")

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	bindEnvs(Config{})
	_ = viper.Unmarshal(&config)

	if raw := viper.GetString("public_origin"); raw != "" {
		config.PublicOrigins = nil
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				config.PublicOrigins = append(config.PublicOrigins, s)
			}
		}
	}
	if config.WebAuthn.RPID == "" && len(config.PublicOrigins) > 0 {
		if u, err := url.Parse(config.PublicOrigins[0]); err == nil && u.Hostname() != "" {
			config.WebAuthn.RPID = u.Hostname()
		}
	}
	if len(config.WebAuthn.RPOrigins) == 0 {
		config.WebAuthn.RPOrigins = config.PublicOrigins
	}

	if config.OIDC.Issuer == "" && len(config.PublicOrigins) > 0 {
		config.OIDC.Issuer = config.PublicOrigins[0]
	}

	// TOTP issuer falls back to the WebAuthn RP display name so deployers
	// only have to set the product name once. Resolved after Unmarshal so
	// an explicit totp.issuer override still wins over the fallback.
	if config.TOTP.Issuer == "" {
		config.TOTP.Issuer = config.WebAuthn.RPDisplayName
	}

	if config.Branding.InstanceName == "" {
		config.Branding.InstanceName = "Prohibitorum"
	}

	if config.ForwardAuth.SessionTTL <= 0 {
		config.ForwardAuth.SessionTTL = time.Hour
	}

	keys, err := loadDataEncryptionKeys(os.Environ())
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("no data encryption keys configured: set at least one PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>")
	}
	config.DataEncryptionKeys = keys

	return &config, nil
}

// dataEncryptionKeyPattern scopes the key-version env var to a positive
// integer suffix; cryptographic length validation happens at use time.
var dataEncryptionKeyPattern = regexp.MustCompile(`^PROHIBITORUM_DATA_ENCRYPTION_KEY_V(\d+)=(.+)$`)

func loadDataEncryptionKeys(env []string) (map[int][]byte, error) {
	out := map[int][]byte{}
	for _, kv := range env {
		m := dataEncryptionKeyPattern.FindStringSubmatch(kv)
		if m == nil {
			continue
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("parse data encryption key version: %w", err)
		}
		raw, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			return nil, fmt.Errorf("decode data encryption key v%d: %w", version, err)
		}
		out[version] = raw
	}
	return out, nil
}

func bindEnvs(iface interface{}, parts ...string) {
	ifv := reflect.ValueOf(iface)
	ift := reflect.TypeOf(iface)
	for i := 0; i < ift.NumField(); i++ {
		v := ifv.Field(i)
		t := ift.Field(i)
		tv, ok := t.Tag.Lookup("mapstructure")
		if !ok {
			continue
		}
		switch v.Kind() {
		case reflect.Struct:
			bindEnvs(v.Interface(), append(parts, tv)...)
		default:
			_ = viper.BindEnv(strings.Join(append(parts, tv), "."))
		}
	}
}
