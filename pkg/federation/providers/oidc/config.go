package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	oidcclient "github.com/zitadel/oidc/v3/pkg/client"
	federationcore "prohibitorum/pkg/federation"
)

type Endpoints struct {
	Authorization *string `json:"authorization"`
	Token         *string `json:"token"`
	UserInfo      *string `json:"userinfo"`
	JWKS          *string `json:"jwks"`
}

type Config struct {
	IssuerURL            string    `json:"issuerUrl"`
	ClientID             string    `json:"clientId"`
	Scopes               []string  `json:"scopes"`
	AllowedDomains       []string  `json:"allowedDomains"`
	UsernameClaim        string    `json:"usernameClaim"`
	DisplayNameClaim     string    `json:"displayNameClaim"`
	EmailClaim           string    `json:"emailClaim"`
	PictureClaim         string    `json:"pictureClaim"`
	RequireVerifiedEmail bool      `json:"requireVerifiedEmail"`
	AllowPrivateNetwork  bool      `json:"allowPrivateNetwork"`
	ConfigurationMode    string    `json:"configurationMode"`
	Endpoints            Endpoints `json:"endpoints"`
	TokenAuthMethod      string    `json:"tokenAuthMethod"`
	PKCEMethod           string    `json:"pkceMethod"`
}

// ResolvedConfig is a credential-free snapshot shared by login and diagnostics.
type ResolvedConfig struct {
	Issuer                string            `json:"issuer"`
	AuthorizationEndpoint string            `json:"authorizationEndpoint"`
	TokenEndpoint         string            `json:"tokenEndpoint"`
	UserInfoEndpoint      string            `json:"userinfoEndpoint"`
	JWKSEndpoint          string            `json:"jwksEndpoint"`
	TokenAuthMethod       string            `json:"tokenAuthMethod"`
	PKCEMethod            string            `json:"pkceMethod"`
	Scopes                []string          `json:"scopes"`
	FetchedAt             time.Time         `json:"fetchedAt"`
	Sources               map[string]string `json:"sources"`
}

// exactObject rejects duplicate, missing and unknown fields before unmarshalling.
func exactObject(raw []byte, keys []string, nullable bool) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errors.New("federation/oidc: config must be an object")
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("invalid field")
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		if !nullable && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("field %q must not be null", key)
		}
		fields[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("unexpected trailing config data")
	}
	if len(fields) != len(keys) {
		return nil, errors.New("federation/oidc: config fields do not match schema")
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("missing config field %q", key)
		}
	}
	return fields, nil
}

func decodeConfig(raw json.RawMessage) (Config, error) {
	fields, err := exactObject(raw, []string{"issuerUrl", "clientId", "scopes", "allowedDomains", "usernameClaim", "displayNameClaim", "emailClaim", "pictureClaim", "requireVerifiedEmail", "allowPrivateNetwork", "configurationMode", "endpoints", "tokenAuthMethod", "pkceMethod"}, false)
	if err != nil {
		return Config{}, err
	}
	if _, err := exactObject(fields["endpoints"], []string{"authorization", "token", "userinfo", "jwks"}, true); err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateEndpoint(raw string, private bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("federation/oidc: invalid endpoint URL")
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return errors.New("federation/oidc: invalid endpoint query")
	}
	if !private {
		return federationcore.ValidateOutboundURL(raw)
	}
	return nil
}

func validateConfig(c Config) error {
	if c.ClientID == "" || len(c.Scopes) == 0 || c.UsernameClaim == "" || c.DisplayNameClaim == "" || c.EmailClaim == "" || c.PictureClaim == "" {
		return errors.New("federation/oidc: client id, scopes and claim names are required")
	}
	if err := validateEndpoint(c.IssuerURL, c.AllowPrivateNetwork); err != nil {
		return err
	}
	switch c.ConfigurationMode {
	case "discovery", "manual":
	default:
		return errors.New("federation/oidc: invalid configuration mode")
	}
	switch c.TokenAuthMethod {
	case "discovery":
		if c.ConfigurationMode != "discovery" {
			return errors.New("federation/oidc: manual mode requires an explicit token auth method")
		}
	case "client_secret_basic", "client_secret_post", "none":
	default:
		return errors.New("federation/oidc: invalid token auth method")
	}
	switch c.PKCEMethod {
	case "S256", "plain", "off":
	default:
		return errors.New("federation/oidc: invalid PKCE method")
	}
	if c.TokenAuthMethod == "none" && c.PKCEMethod != "S256" {
		return errors.New("federation/oidc: public clients require S256")
	}
	for name, endpoint := range map[string]*string{"authorization": c.Endpoints.Authorization, "token": c.Endpoints.Token, "userinfo": c.Endpoints.UserInfo, "jwks": c.Endpoints.JWKS} {
		if endpoint == nil {
			if c.ConfigurationMode == "manual" && name != "userinfo" {
				return fmt.Errorf("federation/oidc: %s endpoint is required", name)
			}
			continue
		}
		if err := validateEndpoint(*endpoint, c.AllowPrivateNetwork); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// ResolveConfig performs a fresh discovery read, or resolves manual configuration
// without network access. It never reads credentials or a login client cache.
func ResolveConfig(ctx context.Context, c Config) (ResolvedConfig, error) {
	if err := validateConfig(c); err != nil {
		return ResolvedConfig{}, err
	}
	r := ResolvedConfig{Issuer: c.IssuerURL, TokenAuthMethod: c.TokenAuthMethod, PKCEMethod: c.PKCEMethod, Scopes: append([]string(nil), c.Scopes...), Sources: map[string]string{"issuer": "manual", "scopes": "manual", "pkceMethod": "manual", "tokenAuthMethod": "manual"}}
	if c.ConfigurationMode == "discovery" {
		d, err := oidcclient.Discover(ctx, c.IssuerURL, federationcore.NewOutboundHTTPClient(c.AllowPrivateNetwork, 2<<20))
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("federation/oidc: discovery: %w", err)
		}
		r.AuthorizationEndpoint, r.TokenEndpoint, r.UserInfoEndpoint, r.JWKSEndpoint = d.AuthorizationEndpoint, d.TokenEndpoint, d.UserinfoEndpoint, d.JwksURI
		r.Sources["tokenAuthMethod"] = "override"
		if c.TokenAuthMethod == "discovery" {
			r.Sources["tokenAuthMethod"] = "discovery"
			r.TokenAuthMethod = ""
			for _, method := range d.TokenEndpointAuthMethodsSupported {
				if string(method) == "client_secret_post" {
					r.TokenAuthMethod = string(method)
				}
			}
			for _, method := range d.TokenEndpointAuthMethodsSupported {
				if string(method) == "client_secret_basic" {
					r.TokenAuthMethod = string(method)
				}
			}
			if d.TokenEndpointAuthMethodsSupported == nil {
				r.TokenAuthMethod = "client_secret_basic"
			}
			if r.TokenAuthMethod == "" {
				return ResolvedConfig{}, errors.New("federation/oidc: discovery offers no supported token authentication method")
			}
		}
	}
	for _, field := range []struct {
		name     string
		override *string
		value    *string
	}{
		{"authorizationEndpoint", c.Endpoints.Authorization, &r.AuthorizationEndpoint}, {"tokenEndpoint", c.Endpoints.Token, &r.TokenEndpoint}, {"userinfoEndpoint", c.Endpoints.UserInfo, &r.UserInfoEndpoint}, {"jwksEndpoint", c.Endpoints.JWKS, &r.JWKSEndpoint},
	} {
		source := c.ConfigurationMode
		if field.override != nil {
			*field.value = *field.override
			if c.ConfigurationMode == "discovery" {
				source = "override"
			}
		}
		r.Sources[field.name] = source
		if *field.value == "" && field.name == "userinfoEndpoint" {
			continue
		}
		if err := validateEndpoint(*field.value, c.AllowPrivateNetwork); err != nil {
			return ResolvedConfig{}, fmt.Errorf("%s: %w", field.name, err)
		}
	}
	r.FetchedAt = time.Now().UTC()
	return r, nil
}
