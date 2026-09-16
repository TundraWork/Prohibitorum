package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
)

func TestUpstreamOIDCEndpointsMigrationPostgres(t *testing.T) {
	baseURL := os.Getenv("PROHIBITORUM_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("PROHIBITORUM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "oidc_endpoints_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") //nolint:errcheck
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	conn, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 31); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(conn, ".", 36); err != nil {
		t.Fatal(err)
	}
	old := `{"issuerUrl":"https://issuer.example","clientId":"client","scopes":["openid"],"allowedDomains":[],"usernameClaim":"preferred_username","displayNameClaim":"name","emailClaim":"email","pictureClaim":"picture","requireVerifiedEmail":true,"allowPrivateNetwork":false}`
	for protocol, config := range map[string]string{"oidc": old, "steam": "{}", "vrchat": "{}"} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO upstream_idp (slug,display_name,protocol,mode,provider_config,key_version) VALUES ($1,$1,$1,'link_only',$2::jsonb,NULL)`, protocol, config); err != nil {
			t.Fatal(err)
		}
	}
	if err := goose.UpTo(conn, ".", 38); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := conn.QueryRowContext(ctx, `SELECT provider_config FROM upstream_idp WHERE slug='oidc'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := (federationoidc.Definition{}).ValidateConfig(raw); err != nil {
		t.Fatalf("migrated config: %v", err)
	}
	var config federationoidc.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.ConfigurationMode != "discovery" || config.TokenAuthMethod != "discovery" || config.PKCEMethod != "S256" || config.Endpoints.Authorization != nil || config.Endpoints.Token != nil || config.Endpoints.UserInfo != nil || config.Endpoints.JWKS != nil || !config.RequireVerifiedEmail {
		t.Fatalf("config=%+v", config)
	}
	for _, protocol := range []string{"steam", "vrchat"} {
		var unchanged bool
		if err := conn.QueryRowContext(ctx, `SELECT provider_config='{}'::jsonb FROM upstream_idp WHERE slug=$1`, protocol).Scan(&unchanged); err != nil || !unchanged {
			t.Fatalf("%s changed: %v", protocol, err)
		}
	}
	if err := goose.DownTo(conn, ".", 36); err != nil {
		t.Fatal(err)
	}
	var restored bool
	if err := conn.QueryRowContext(ctx, `SELECT provider_config=$1::jsonb FROM upstream_idp WHERE slug='oidc'`, old).Scan(&restored); err != nil || !restored {
		t.Fatalf("rollback: restored=%v err=%v", restored, err)
	}
}
