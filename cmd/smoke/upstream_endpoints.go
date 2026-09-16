package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"prohibitorum/cmd/smoke/mockop"
)

// smokeUpstreamEndpoints exercises stored configs and the real server's complete
// provisioning/session flow. The provider client tests separately inspect the
// exact token request credentials and verify that discovery is never called.
func smokeUpstreamEndpoints(base, issuer string, op *mockop.Server, dek []byte) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("PROHIBITORUM_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for i, mode := range []struct{ auth, pkce, configuration string }{
		{"client_secret_basic", "S256", "manual"}, {"client_secret_basic", "plain", "manual"}, {"client_secret_basic", "off", "manual"},
		{"client_secret_post", "S256", "manual"}, {"client_secret_post", "plain", "manual"}, {"client_secret_post", "off", "manual"},
		{"none", "S256", "manual"}, {"client_secret_post", "plain", "discovery"},
	} {
		slug := fmt.Sprintf("endpoint-mode-%d", i)
		if _, err := seedUpstreamIDP(dek, slug, slug, issuer, "endpoint-client", "secret", "auto_provision", nil, true, true); err != nil {
			return err
		}
		raw, err := oidcSeedProviderConfig(issuer, "endpoint-client", nil, true, true)
		if err != nil {
			return err
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			return err
		}
		config["configurationMode"] = mode.configuration
		config["tokenAuthMethod"] = mode.auth
		config["pkceMethod"] = mode.pkce
		config["endpoints"] = map[string]any{"authorization": issuer + "/authorize?custom=1", "token": issuer + "/token", "userinfo": nil, "jwks": issuer + "/jwks"}
		raw, err = json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "UPDATE upstream_idp SET provider_config=$1 WHERE slug=$2", raw, slug); err != nil {
			return err
		}
		if mode.auth == "none" {
			if _, err := conn.Exec(ctx, "UPDATE upstream_idp SET secret_enc=NULL,secret_nonce=NULL,key_version=NULL,secret_status='unconfigured' WHERE slug=$1", slug); err != nil {
				return err
			}
		}
		op.SetClaims(slug, slug+"@example.com", true, slug, slug)
		browser, err := newFederationClient(base)
		if err != nil {
			return err
		}
		if err := driveFederationToWelcome(browser, base, slug); err != nil {
			return fmt.Errorf("%s %s %s: %w", mode.configuration, mode.auth, mode.pkce, err)
		}
		if _, err := browser.confirmPost(); err != nil {
			return err
		}
		me, err := browser.getMe()
		if err != nil {
			return err
		}
		if me.Username != slug {
			return fmt.Errorf("%s returned wrong account", slug)
		}
		if err := driveFederationLogin(browser, base, slug, "/me"); err != nil {
			return err
		}
		log.Printf("  %s %s %s → confirmed account, session and repeat login ✓", mode.configuration, mode.auth, mode.pkce)
	}
	return nil
}
