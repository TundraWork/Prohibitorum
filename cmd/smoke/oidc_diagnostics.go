package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"prohibitorum/cmd/smoke/mockop"
)

func smokeOIDCDiagnostics(admin *client, base string, op *mockop.Server) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("PROHIBITORUM_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var accountsBefore, identitiesBefore int64
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM account),(SELECT count(*) FROM account_identity)").Scan(&accountsBefore, &identitiesBefore); err != nil {
		return err
	}
	meBefore, err := admin.getMe()
	if err != nil {
		return err
	}
	baseURL, _ := url.Parse(base)
	cookiesBefore := fmt.Sprint(admin.jar.Cookies(baseURL))
	op.SetClaims("diagnostic-only-subject", "diagnostic-only@example.com", true, "diagnostic-only-user", "Diagnostic Only")
	const path = "/api/prohibitorum/identity-providers/mockop"
	effectiveReq, _ := http.NewRequest(http.MethodGet, base+path+"/effective-config", nil)
	var effective struct {
		CallbackURL string
		Fields      map[string]any
	}
	if err := admin.do(effectiveReq, &effective); err != nil {
		return err
	}
	expectedCallback := base + "/api/prohibitorum/auth/federation/mockop/test/callback"
	if effective.CallbackURL != expectedCallback || len(effective.Fields) != 8 {
		return fmt.Errorf("unexpected effective config")
	}
	var start struct{ ID, AuthorizationURL string }
	if err := admin.postJSON(path+"/tests", nil, &start); err != nil {
		return err
	}
	authorization, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		return err
	}
	if authorization.Query().Get("redirect_uri") != expectedCallback {
		return fmt.Errorf("test reused normal callback")
	}
	callback, err := followMockOPAuthorize(start.AuthorizationURL)
	if err != nil {
		return err
	}
	// Normal login state cannot enter the test callback namespace.
	wrong := base + "/api/prohibitorum/auth/federation/mockop/test/callback?state=normal-flow&code=x"
	response, err := admin.hc.Get(wrong)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode < 400 {
		return fmt.Errorf("test callback accepted ordinary state")
	}

	callbackURL, _ := url.Parse(callback)
	normalCallback := base + "/api/prohibitorum/auth/federation/mockop/callback?" + callbackURL.RawQuery
	normalDestination, normalErr := admin.getRedirectAbs(normalCallback)
	if normalErr != nil || !strings.Contains(normalDestination, "federation_state_invalid") {
		return fmt.Errorf("normal callback accepted test state")
	}
	destination, err := admin.getRedirectAbs(callback)
	if err != nil {
		return err
	}
	u, err := url.Parse(destination)
	if err != nil {
		return err
	}
	if len(u.Query()) != 1 || u.Query().Get("test") != start.ID || strings.Contains(destination, "code=") {
		return fmt.Errorf("callback exposed data")
	}
	var result struct {
		Status string
		Claims map[string]any
		Stages []map[string]any
	}
	if err := admin.postJSON(path+"/tests/"+start.ID+"/complete", nil, &result); err != nil {
		return err
	}
	if result.Status != "succeeded" || result.Claims["subject"] != "diagnostic-only-subject" || len(result.Stages) != 6 {
		return fmt.Errorf("diagnostic result: status=%s stages=%d", result.Status, len(result.Stages))
	}
	if err := admin.postJSON(path+"/tests/"+start.ID+"/complete", nil, &result); err != nil {
		return err
	}
	readReq, _ := http.NewRequest(http.MethodGet, base+path+"/tests/"+start.ID, nil)
	if err := admin.do(readReq, &result); err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return fmt.Errorf("result not durable")
	}
	wire, _ := json.Marshal(result)
	for _, forbidden := range []string{"access_token", "refresh_token", "raw_id_token", "client_secret", "code_verifier"} {
		if strings.Contains(string(wire), forbidden) {
			return fmt.Errorf("diagnostic exposed %s", forbidden)
		}
	}
	var accountsAfter, identitiesAfter int64
	if err := conn.QueryRow(ctx, "SELECT (SELECT count(*) FROM account),(SELECT count(*) FROM account_identity)").Scan(&accountsAfter, &identitiesAfter); err != nil {
		return err
	}
	if accountsBefore != accountsAfter || identitiesBefore != identitiesAfter {
		return fmt.Errorf("diagnostic changed accounts or identities")
	}
	meAfter, err := admin.getMe()
	if err != nil {
		return err
	}
	if meBefore.ID != meAfter.ID {
		return fmt.Errorf("diagnostic changed admin session")
	}
	// Only the separate test binding cookie can be added; existing session
	// cookies remain byte-identical.
	for _, cookie := range admin.jar.Cookies(baseURL) {
		if cookie.Name != "prohibitorum_oidc_test" && !strings.Contains(cookiesBefore, cookie.String()) {
			return fmt.Errorf("diagnostic changed session cookie")
		}
	}
	log.Printf("  effective config + separate callback + exchange + repeat/read: no account/identity writes, admin session unchanged ✓")
	return nil
}
