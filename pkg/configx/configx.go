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
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`

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

type KVConfig struct {
	Driver   string `mapstructure:"driver"`
	RedisURL string `mapstructure:"redis_url"`
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
	// JWKSCacheMaxAge is advertised in the discovery doc as a hint to
	// downstream RPs; default 1h.
	JWKSCacheMaxAge time.Duration `mapstructure:"jwks_cache_max_age"`
}

// FederationConfig governs upstream-IdP federation flows.
type FederationConfig struct {
	StateTTL      time.Duration `mapstructure:"state_ttl"`
	DefaultScopes []string      `mapstructure:"default_scopes"`
}

// TOTPConfig holds RFC 6238 enrollment defaults + the drift window used
// during verification.
type TOTPConfig struct {
	DefaultPeriod     int    `mapstructure:"default_period"`
	DefaultDigits     int    `mapstructure:"default_digits"`
	DefaultAlgorithm  string `mapstructure:"default_algorithm"`
	DriftSteps        int    `mapstructure:"drift_steps"`
	RecoveryCodeCount int    `mapstructure:"recovery_code_count"`
}

// SAMLConfig holds IdP identity + the per-deployment defaults that apply
// to all registered SPs (SPs may override per-row).
type SAMLConfig struct {
	EntityID              string        `mapstructure:"entity_id"`
	BaseURL               string        `mapstructure:"base_url"`
	DefaultNameIDFormat   string        `mapstructure:"default_nameid_format"`
	SessionLifetime       time.Duration `mapstructure:"session_lifetime"`
	MetadataRotationGrace time.Duration `mapstructure:"metadata_rotation_grace"`
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
	viper.SetDefault("oidc.jwks_cache_max_age", time.Hour)

	// Federation defaults.
	viper.SetDefault("federation.state_ttl", 10*time.Minute)
	viper.SetDefault("federation.default_scopes", []string{"openid", "profile", "email"})

	// TOTP defaults — RFC 6238 §5.2 baseline, ±1 step drift window,
	// 10 recovery codes per account.
	viper.SetDefault("totp.default_period", 30)
	viper.SetDefault("totp.default_digits", 6)
	viper.SetDefault("totp.default_algorithm", "SHA1")
	viper.SetDefault("totp.drift_steps", 1)
	viper.SetDefault("totp.recovery_code_count", 10)

	// SAML defaults — persistent NameID per OASIS SAML 2.0 Core §8.3.7,
	// 7d metadata rotation grace.
	viper.SetDefault("saml.entity_id", "")
	viper.SetDefault("saml.base_url", "")
	viper.SetDefault("saml.default_nameid_format", "urn:oasis:names:tc:SAML:1.1:nameid-format:persistent")
	viper.SetDefault("saml.session_lifetime", 8*time.Hour)
	viper.SetDefault("saml.metadata_rotation_grace", 7*24*time.Hour)

	// Password hashing defaults — 64 MiB / 3 iterations / 4 lanes is the
	// OWASP-recommended argon2id baseline for interactive auth.
	viper.SetDefault("password_hash.memory_kib", 65536)
	viper.SetDefault("password_hash.iterations", 3)
	viper.SetDefault("password_hash.parallelism", 4)

	// WebAuthn substruct defaults — RPDisplayName defaults to the product name;
	// RPID and RPOrigins are derived from PublicOrigins when not set explicitly.
	viper.SetDefault("webauthn.rp_display_name", "Prohibitorum")
	viper.SetDefault("webauthn.rp_id", "")

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
