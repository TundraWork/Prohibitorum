package configx

import (
	"errors"
	"net/url"
	"reflect"
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
	WebAuthnRPID  string        `mapstructure:"webauthn_rp_id"`
	SessionTTL    time.Duration `mapstructure:"session_ttl"`
	TrustProxy    bool          `mapstructure:"trust_proxy"`

	KV   KVConfig   `mapstructure:"kv"`
	OIDC OIDCConfig `mapstructure:"oidc"`
}

type KVConfig struct {
	Driver   string `mapstructure:"driver"`
	RedisURL string `mapstructure:"redis_url"`
}

// OIDCConfig governs token lifetimes + the issuer string. The issuer
// should be the public-facing origin (e.g. https://auth.example.com)
// and is embedded in every signed token + the discovery doc.
type OIDCConfig struct {
	Issuer             string        `mapstructure:"issuer"`
	AccessTokenTTL     time.Duration `mapstructure:"access_token_ttl"`
	IDTokenTTL         time.Duration `mapstructure:"id_token_ttl"`
	RefreshTokenTTL    time.Duration `mapstructure:"refresh_token_ttl"`
	AuthorizationCodeTTL time.Duration `mapstructure:"authorization_code_ttl"`
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
	viper.SetDefault("webauthn_rp_id", "")
	viper.SetDefault("session_ttl", 8*time.Hour)
	viper.SetDefault("trust_proxy", false)

	// OIDC defaults — short access tokens, longer refresh tokens, very
	// short authorization codes (single-use anyway).
	viper.SetDefault("oidc.issuer", "")
	viper.SetDefault("oidc.access_token_ttl", 10*time.Minute)
	viper.SetDefault("oidc.id_token_ttl", 10*time.Minute)
	viper.SetDefault("oidc.refresh_token_ttl", 720*time.Hour) // 30d
	viper.SetDefault("oidc.authorization_code_ttl", 60*time.Second)

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
	if config.WebAuthnRPID == "" && len(config.PublicOrigins) > 0 {
		if u, err := url.Parse(config.PublicOrigins[0]); err == nil && u.Hostname() != "" {
			config.WebAuthnRPID = u.Hostname()
		}
	}
	if config.OIDC.Issuer == "" && len(config.PublicOrigins) > 0 {
		config.OIDC.Issuer = config.PublicOrigins[0]
	}

	return &config, nil
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
