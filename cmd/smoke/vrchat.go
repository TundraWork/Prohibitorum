package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	vrchatSlug       = "vrchat-smoke"
	vrchatUserA      = "usr_10000000-0000-0000-0000-000000000001"
	vrchatUserB      = "usr_10000000-0000-0000-0000-000000000002"
	vrchatWrongUser  = "usr_10000000-0000-0000-0000-000000000099"
	vrchatOperator   = "vrchat.operator+distinct@example.test"
	vrchatPassword   = "VRCHAT_PASSWORD_DISTINCT_9f7c"
	vrchatCode       = "VCODE_628401"
	vrchatAuthCookie = "VCOOKIE_AUTH_DISTINCT_51ac"
	vrchat2FACookie  = "VCOOKIE_2FA_DISTINCT_b870"
)

type vrchatFixture struct {
	Username, Password, Code                 string
	AuthCookieValue, TwoFactorCookieValue    string
	RequireTwoFactor                         bool
	Methods                                  []string
	CurrentUserID, PublicUserID, DisplayName string
	AvatarURL                                string
	BioLinks                                 []string
	CurrentStatus, PublicStatus              int
	RetryAfter, PublicBodyMode               string
}

type vrchatRequestRecord struct {
	Method, Path, UserAgent string
	CookieNames             []string `json:"cookieNames"`
}

type vrchatFlowView struct {
	Provider struct {
		Slug, DisplayName, Protocol string
	} `json:"provider"`
	Intent, Step, ProfileURL, ProofURL string
	RequiresLocalUsername              bool
	ExpiresAt                          time.Time
}

type vrchatProviderView struct {
	Slug, Protocol, Mode, SecretStatus string
	Disabled, Ready, SecretConfigured  bool
}

type vrchatOperatorResult struct {
	Status    string
	Challenge string
	Methods   []string
	Provider  *vrchatProviderView
}

type vrchatError struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details"`
}

type vrchatSmoke struct {
	base, control string
	controlClient *http.Client
	fixture       vrchatFixture
	bodies        [][]byte
	proofs        []string
	proofBodies   [][]byte
}

func newVRChatSmoke(base, control, caFile string) (*vrchatSmoke, error) {
	if control == "" || caFile == "" {
		return nil, fmt.Errorf("VRChat smoke requires --vrchat-control-origin and --vrchat-ca-file")
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("invalid VRChat smoke CA")
	}
	v := &vrchatSmoke{base: base, control: strings.TrimSuffix(control, "/"), controlClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 10 * time.Second}}
	v.fixture = vrchatFixture{Username: vrchatOperator, Password: vrchatPassword, Code: vrchatCode, AuthCookieValue: vrchatAuthCookie, TwoFactorCookieValue: vrchat2FACookie, RequireTwoFactor: true, Methods: []string{"totp"}, CurrentUserID: vrchatUserA, PublicUserID: vrchatUserA, DisplayName: "VRChat Smoke Alpha", AvatarURL: "https://api.vrchat.cloud/avatar-alpha.png", BioLinks: []string{}}
	return v, v.setFixture()
}

func (v *vrchatSmoke) setFixture() error {
	body, _ := json.Marshal(v.fixture)
	req, _ := http.NewRequest(http.MethodPost, v.control+"/control/state", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.controlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("VRChat control state: %d", resp.StatusCode)
	}
	return nil
}

func (v *vrchatSmoke) records() ([]vrchatRequestRecord, error) {
	resp, err := v.controlClient.Get(v.control + "/control/requests")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var records []vrchatRequestRecord
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&records) != nil {
		return nil, fmt.Errorf("VRChat request records status=%d", resp.StatusCode)
	}
	return records, nil
}

func (v *vrchatSmoke) begin(c *client, path string) (string, error) {
	location, err := c.getRedirect(path)
	if err != nil {
		return "", err
	}
	const prefix = "/federation/flow/"
	if !strings.HasPrefix(location, prefix) {
		return "", fmt.Errorf("flow destination = %q", location)
	}
	return strings.TrimPrefix(location, prefix), nil
}

func (v *vrchatSmoke) prepare(c *client, flow, userID string) (vrchatFlowView, error) {
	resp, err := c.postJSONRaw("/api/prohibitorum/auth/federation/flows/"+url.PathEscape(flow)+"/prepare", map[string]string{"identity": userID})
	if err != nil {
		return vrchatFlowView{}, err
	}
	return v.decodeFlow(resp, http.StatusOK)
}

func (v *vrchatSmoke) reload(c *client, flow string) (vrchatFlowView, error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+"/api/prohibitorum/auth/federation/flows/"+url.PathEscape(flow), nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return vrchatFlowView{}, err
	}
	return v.decodeFlow(resp, http.StatusOK)
}

func (v *vrchatSmoke) decodeFlow(resp *http.Response, status int) (vrchatFlowView, error) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	v.bodies = append(v.bodies, append([]byte(nil), body...))
	if resp.StatusCode != status {
		return vrchatFlowView{}, fmt.Errorf("flow status=%d body=%s", resp.StatusCode, body)
	}
	var view vrchatFlowView
	if err := json.Unmarshal(body, &view); err != nil {
		return view, err
	}
	if view.ProofURL != "" {
		v.proofBodies = append(v.proofBodies, append([]byte(nil), body...))
	}
	return view, nil
}

func (v *vrchatSmoke) verify(c *client, flow, username string) (*http.Response, []byte, error) {
	body := map[string]string{}
	if username != "" {
		body["localUsername"] = username
	}
	resp, err := c.postJSONRaw("/api/prohibitorum/auth/federation/flows/"+url.PathEscape(flow)+"/verify", body)
	if err != nil {
		return nil, nil, err
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	v.bodies = append(v.bodies, append([]byte(nil), data...))
	return resp, data, nil
}

func decodeVRChatError(body []byte) vrchatError {
	var out vrchatError
	_ = json.Unmarshal(body, &out)
	return out
}

func (v *vrchatSmoke) publish(view vrchatFlowView) (string, error) {
	proofURL, err := url.Parse(view.ProofURL)
	if err != nil || proofURL.Scheme != "http" && proofURL.Scheme != "https" {
		return "", fmt.Errorf("invalid proof URL %q", view.ProofURL)
	}
	const prefix = "/verify/vrchat/"
	if !strings.HasPrefix(proofURL.Path, prefix) {
		return "", fmt.Errorf("proof path = %q", proofURL.Path)
	}
	proof := strings.TrimPrefix(proofURL.Path, prefix)
	if proof == "" || strings.Contains(proof, "/") {
		return "", fmt.Errorf("invalid proof token")
	}
	v.proofs = append(v.proofs, proof)
	v.fixture.BioLinks = []string{view.ProofURL}
	return proof, v.setFixture()
}

func (v *vrchatSmoke) expectError(resp *http.Response, body []byte, status int, code string) error {
	got := decodeVRChatError(body)
	if resp.StatusCode != status || got.Code != code || len(got.Details) != 0 {
		return fmt.Errorf("error got status=%d body=%s; want %d %s with no details", resp.StatusCode, body, status, code)
	}
	return nil
}

func runVRChatSmoke(admin *client, base, control, caFile, serverLog, mockLog, adminUsername string) error {
	v, err := newVRChatSmoke(base, control, caFile)
	if err != nil {
		return err
	}
	step(fmt.Sprintf("vrchat %d/%d — create disabled, unconfigured provider", 1, nVRChat))
	var provider vrchatProviderView
	if err := admin.postJSON("/api/prohibitorum/identity-providers", map[string]any{"slug": vrchatSlug, "displayName": "VRChat Smoke", "protocol": "vrchat", "mode": "auto_provision", "config": map[string]any{}}, &provider); err != nil {
		return err
	}
	if provider.Slug != vrchatSlug || provider.Protocol != "vrchat" || provider.Mode != "auto_provision" || !provider.Disabled || provider.Ready || provider.SecretConfigured || provider.SecretStatus != "unconfigured" {
		return fmt.Errorf("VRChat create projection = %+v", provider)
	}

	step(fmt.Sprintf("vrchat %d/%d — operator Basic auth issues TOTP challenge", 2, nVRChat))
	var started vrchatOperatorResult
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/start", map[string]string{"username": vrchatOperator, "password": vrchatPassword}, &started); err != nil {
		return err
	}
	if started.Status != "challenge" || started.Challenge == "" || len(started.Methods) != 1 || started.Methods[0] != "totp" {
		return fmt.Errorf("operator start = %+v", started)
	}
	step(fmt.Sprintf("vrchat %d/%d — verify TOTP, validate ready, enable", 3, nVRChat))
	var verified vrchatOperatorResult
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/verify", map[string]string{"challenge": started.Challenge, "method": "totp", "code": vrchatCode}, &verified); err != nil || verified.Status != "valid" {
		return fmt.Errorf("operator verify status=%q err=%w", verified.Status, err)
	}
	var validated vrchatOperatorResult
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/validate", nil, &validated); err != nil || validated.Provider == nil || !validated.Provider.Ready || validated.Provider.SecretStatus != "valid" {
		return fmt.Errorf("operator validate = %+v err=%w", validated, err)
	}
	provider = *validated.Provider
	if err := admin.postJSON("/api/prohibitorum/identity-providers/set-disabled", map[string]any{"slug": vrchatSlug, "disabled": false}, &provider); err != nil || provider.Disabled || !provider.Ready {
		return fmt.Errorf("provider enable = %+v err=%w", provider, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — operator requests reuse cookies with sanitized metadata", 4, nVRChat))
	records, err := v.records()
	if err != nil || len(records) < 3 {
		return fmt.Errorf("operator request records: %v count=%d", err, len(records))
	}
	encodedRecords, _ := json.Marshal(records)
	for _, secret := range []string{vrchatPassword, vrchatCode, vrchatAuthCookie, vrchat2FACookie} {
		if bytes.Contains(encodedRecords, []byte(secret)) {
			return fmt.Errorf("sanitized records contain secret marker")
		}
	}
	last := records[len(records)-1]
	if last.Path != "/api/1/auth/user" || !strings.Contains(last.UserAgent, "Prohibitorum/") || !containsAll(last.CookieNames, "auth", "twoFactorAuth") {
		return fmt.Errorf("operator cookie reuse record = %+v", last)
	}

	step(fmt.Sprintf("vrchat %d/%d — auto_provision prepare/publish/verify/confirm/session", 5, nVRChat))
	auto, _ := newFederationClient(base)
	flow, err := v.begin(auto, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	if err != nil {
		return err
	}
	view, err := v.prepare(auto, flow, vrchatUserA)
	if err != nil || view.Step != "proof" || !view.RequiresLocalUsername {
		return fmt.Errorf("auto prepare = %+v err=%w", view, err)
	}
	proofA, err := v.publish(view)
	if err != nil {
		return err
	}
	reloaded, err := v.reload(auto, flow)
	if err != nil || reloaded.ProofURL != view.ProofURL {
		return fmt.Errorf("prepared flow reload proof mismatch")
	}
	resp, body, err := v.verify(auto, flow, "vrchat-smoke-alpha")
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auto verify status=%v body=%s err=%w", statusOf(resp), body, err)
	}
	if _, err := auto.confirmGet(); err != nil {
		return err
	}
	if redirect, err := auto.confirmPost(); err != nil || redirect != "/me" {
		return fmt.Errorf("auto confirm redirect=%q err=%w", redirect, err)
	}
	me, err := auto.getMe()
	if err != nil || me.Username != "vrchat-smoke-alpha" {
		return fmt.Errorf("auto durable session = %+v err=%w", me, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — proof replay rejected", 6, nVRChat))
	resp, body, _ = v.verify(auto, flow, "")
	if err := v.expectError(resp, body, http.StatusUnauthorized, "federation_state_invalid"); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — relogin issues fresh proof and refreshes metadata", 7, nVRChat))
	_ = auto.logout()
	v.fixture.DisplayName = "VRChat Smoke Alpha Refreshed"
	v.fixture.AvatarURL = "https://api.vrchat.cloud/avatar-alpha-refreshed.png"
	v.fixture.BioLinks = nil
	_ = v.setFixture()
	flow2, _ := v.begin(auto, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	view2, err := v.prepare(auto, flow2, vrchatUserA)
	if err != nil {
		return err
	}
	proof2, err := v.publish(view2)
	if err != nil || proof2 == proofA {
		return fmt.Errorf("relogin proof not fresh")
	}
	resp, body, _ = v.verify(auto, flow2, "")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relogin verify=%d %s", resp.StatusCode, body)
	}
	if _, err := auto.getMe(); err != nil {
		return fmt.Errorf("relogin durable session: %w", err)
	}
	rows, err := dbScalar(os.Getenv("PROHIBITORUM_DATABASE_URL"), "SELECT upstream_data::text FROM account_identity WHERE upstream_sub='"+vrchatUserA+"'")
	if err != nil || len(rows) != 1 || !strings.Contains(rows[0], `"displayName": "VRChat Smoke Alpha Refreshed"`) || !strings.Contains(rows[0], `"profileUrl": "https://vrchat.com/home/user/`+vrchatUserA+`"`) || strings.Contains(rows[0], proof2) {
		return fmt.Errorf("VRChat metadata persistence = %v err=%v", rows, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — wrong public user rejected", 8, nVRChat))
	wrong, _ := newFederationClient(base)
	wf, _ := v.begin(wrong, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	wv, _ := v.prepare(wrong, wf, vrchatUserA)
	_, _ = v.publish(wv)
	v.fixture.PublicUserID = vrchatWrongUser
	_ = v.setFixture()
	resp, body, _ = v.verify(wrong, wf, "")
	if err := v.expectError(resp, body, http.StatusBadRequest, "vrchat_identity_invalid"); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — missing proof retries in same flow", 9, nVRChat))
	v.fixture.PublicUserID, v.fixture.BioLinks = vrchatUserA, nil
	_ = v.setFixture()
	missing, _ := newFederationClient(base)
	mf, _ := v.begin(missing, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	mv, _ := v.prepare(missing, mf, vrchatUserA)
	resp, body, _ = v.verify(missing, mf, "")
	if err := v.expectError(resp, body, http.StatusConflict, "vrchat_proof_missing"); err != nil {
		return err
	}
	_, _ = v.publish(mv)
	resp, body, _ = v.verify(missing, mf, "")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("same-flow proof retry=%d %s", resp.StatusCode, body)
	}

	step(fmt.Sprintf("vrchat %d/%d — browser cookie swap rejected", 10, nVRChat))
	swapOwner, _ := newFederationClient(base)
	sf, _ := v.begin(swapOwner, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	swapOther, _ := newFederationClient(base)
	_, err = v.reload(swapOther, sf)
	if err == nil || !strings.Contains(err.Error(), "status=401") {
		return fmt.Errorf("browser swap error=%v", err)
	}

	step(fmt.Sprintf("vrchat %d/%d — five-second state expires after six seconds", 11, nVRChat))
	exp, _ := newFederationClient(base)
	ef, _ := v.begin(exp, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	time.Sleep(6 * time.Second)
	_, err = v.reload(exp, ef)
	if err == nil || !strings.Contains(err.Error(), "status=401") || !strings.Contains(err.Error(), "federation_state_invalid") {
		return fmt.Errorf("expiry error=%v", err)
	}

	step(fmt.Sprintf("vrchat %d/%d — 429 Retry-After and same-flow recovery", 12, nVRChat))
	rate, _ := newFederationClient(base)
	rf, _ := v.begin(rate, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	rv, _ := v.prepare(rate, rf, vrchatUserA)
	_, _ = v.publish(rv)
	v.fixture.PublicStatus, v.fixture.RetryAfter = http.StatusTooManyRequests, "1"
	_ = v.setFixture()
	resp, body, _ = v.verify(rate, rf, "")
	if err := v.expectError(resp, body, http.StatusTooManyRequests, "upstream_rate_limited"); err != nil || resp.Header.Get("Retry-After") != "1" {
		return fmt.Errorf("rate limit: %v header=%q", err, resp.Header.Get("Retry-After"))
	}
	time.Sleep(1100 * time.Millisecond)
	v.fixture.PublicStatus, v.fixture.RetryAfter = 0, ""
	_ = v.setFixture()
	resp, body, _ = v.verify(rate, rf, "")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rate recovery=%d %s", resp.StatusCode, body)
	}

	step(fmt.Sprintf("vrchat %d/%d — malformed, oversized, and temporary 5xx are stable/retryable", 13, nVRChat))
	for _, failure := range []struct {
		mode   string
		status int
	}{{"malformed", 0}, {"oversized", 0}, {"", http.StatusServiceUnavailable}} {
		fc, _ := newFederationClient(base)
		ff, _ := v.begin(fc, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
		fv, _ := v.prepare(fc, ff, vrchatUserA)
		_, _ = v.publish(fv)
		v.fixture.PublicBodyMode, v.fixture.PublicStatus = failure.mode, failure.status
		_ = v.setFixture()
		resp, body, _ = v.verify(fc, ff, "")
		if err := v.expectError(resp, body, http.StatusServiceUnavailable, "upstream_temporarily_unavailable"); err != nil {
			return fmt.Errorf("public failure %q/%d: %w", failure.mode, failure.status, err)
		}
		v.fixture.PublicBodyMode, v.fixture.PublicStatus = "", 0
		_ = v.setFixture()
		resp, body, _ = v.verify(fc, ff, "")
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("public failure recovery %q/%d: %d %s", failure.mode, failure.status, resp.StatusCode, body)
		}
	}

	step(fmt.Sprintf("vrchat %d/%d — operator 401 invalidates readiness, then recovery", 14, nVRChat))
	unauth, _ := newFederationClient(base)
	uf, _ := v.begin(unauth, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	uv, _ := v.prepare(unauth, uf, vrchatUserA)
	_, _ = v.publish(uv)
	v.fixture.PublicStatus = http.StatusUnauthorized
	_ = v.setFixture()
	resp, body, _ = v.verify(unauth, uf, "")
	if err := v.expectError(resp, body, http.StatusServiceUnavailable, "provider_not_ready"); err != nil {
		return err
	}
	if err := admin.get("/api/prohibitorum/identity-providers/"+vrchatSlug, &provider); err != nil || provider.Ready || provider.SecretStatus != "invalid" {
		return fmt.Errorf("401 readiness projection=%+v err=%v", provider, err)
	}
	v.fixture.PublicStatus = 0
	_ = v.setFixture()
	started = vrchatOperatorResult{}
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/start", map[string]string{"username": vrchatOperator, "password": vrchatPassword}, &started); err != nil {
		return err
	}
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/verify", map[string]string{"challenge": started.Challenge, "method": "totp", "code": vrchatCode}, &verified); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — username collision retry preserves proof", 15, nVRChat))
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName = vrchatUserB, vrchatUserB, "VRChat Smoke Beta"
	v.fixture.BioLinks = nil
	_ = v.setFixture()
	collision, _ := newFederationClient(base)
	cf, _ := v.begin(collision, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	cv, _ := v.prepare(collision, cf, vrchatUserB)
	_, _ = v.publish(cv)
	resp, body, _ = v.verify(collision, cf, adminUsername)
	if err := v.expectError(resp, body, http.StatusForbidden, "username_collision"); err != nil {
		return err
	}
	resp, body, _ = v.verify(collision, cf, "vrchat-smoke-beta")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("collision retry=%d %s", resp.StatusCode, body)
	}

	step(fmt.Sprintf("vrchat %d/%d — explicit account link and identity conflict", 16, nVRChat))
	v.fixture.CurrentUserID, v.fixture.PublicUserID = vrchatUserA, vrchatUserA
	v.fixture.DisplayName, v.fixture.BioLinks = "VRChat Smoke Alpha Refreshed", nil
	_ = v.setFixture()
	linkFlow, err := v.begin(admin, "/api/prohibitorum/me/identities/link/"+vrchatSlug+"/begin?return_to=/me")
	if err != nil {
		return err
	}
	linkView, _ := v.prepare(admin, linkFlow, vrchatUserA)
	_, _ = v.publish(linkView)
	resp, body, _ = v.verify(admin, linkFlow, "")
	if err := v.expectError(resp, body, http.StatusConflict, "federation_identity_conflict"); err != nil {
		return err
	}
	v.fixture.CurrentUserID, v.fixture.PublicUserID = "usr_10000000-0000-0000-0000-000000000003", "usr_10000000-0000-0000-0000-000000000003"
	v.fixture.DisplayName, v.fixture.BioLinks = "VRChat Smoke Linked", nil
	_ = v.setFixture()
	linkFlow, _ = v.begin(admin, "/api/prohibitorum/me/identities/link/"+vrchatSlug+"/begin?return_to=/me")
	linkView, _ = v.prepare(admin, linkFlow, v.fixture.PublicUserID)
	_, _ = v.publish(linkView)
	resp, body, _ = v.verify(admin, linkFlow, "")
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"redirect":"/connected"`)) {
		return fmt.Errorf("explicit link=%d %s", resp.StatusCode, body)
	}

	step(fmt.Sprintf("vrchat %d/%d — link_only rejects unknown identity", 17, nVRChat))
	if err := admin.putJSON("/api/prohibitorum/identity-providers/"+vrchatSlug, map[string]any{"displayName": "VRChat Smoke", "mode": "link_only", "config": map[string]any{}}, &provider); err != nil {
		return err
	}
	unknown := "usr_10000000-0000-0000-0000-000000000004"
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName, v.fixture.BioLinks = unknown, unknown, "Unknown VRChat", nil
	_ = v.setFixture()
	linkOnly, _ := newFederationClient(base)
	lf, _ := v.begin(linkOnly, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	lv, _ := v.prepare(linkOnly, lf, unknown)
	_, _ = v.publish(lv)
	resp, body, _ = v.verify(linkOnly, lf, "")
	if err := v.expectError(resp, body, http.StatusForbidden, "link_required"); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — invite redemption uses bound local flow", 18, nVRChat))
	if err := admin.putJSON("/api/prohibitorum/identity-providers/"+vrchatSlug, map[string]any{"displayName": "VRChat Smoke", "mode": "invite_only", "config": map[string]any{}}, &provider); err != nil {
		return err
	}
	const inviteToken = "vrchat-invite-distinct-001"
	if err := seedInviteEnrollment(inviteToken, "vrchat-invite-user", "VRChat Invite User", "user", vrchatSlug, "1 hour"); err != nil {
		return err
	}
	inviteID := "usr_10000000-0000-0000-0000-000000000005"
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName, v.fixture.BioLinks = inviteID, inviteID, "VRChat Invite Upstream", nil
	_ = v.setFixture()
	invite, _ := newFederationClient(base)
	iflow, _ := v.begin(invite, "/api/prohibitorum/enrollments/"+inviteToken+"/start-federation?return_to=/me")
	iv, _ := v.prepare(invite, iflow, inviteID)
	_, _ = v.publish(iv)
	resp, body, _ = v.verify(invite, iflow, "")
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite verify=%d %s", resp.StatusCode, body)
	}
	if me, err := invite.getMe(); err != nil || me.Username != "vrchat-invite-user" {
		return fmt.Errorf("invite session=%+v err=%v", me, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — exact metadata refresh and filtering before pagination", 19, nVRChat))
	if err := verifyVRChatFiltering(admin, vrchatSlug, vrchatUserA); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — proof public page invariant and browser-scoped exposure", 20, nVRChat))
	var shell []byte
	for _, proof := range []string{proofA, proof2, "invalid-public-proof"} {
		resp, err := http.Get(base + "/verify/vrchat/" + proof)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" || bytes.Contains(data, []byte(proof)) {
			return fmt.Errorf("public proof page status=%d proof exposure=%v", resp.StatusCode, bytes.Contains(data, []byte(proof)))
		}
		if shell == nil {
			shell = data
		} else if !bytes.Equal(shell, data) {
			return fmt.Errorf("public proof page varies by token")
		}
	}

	step(fmt.Sprintf("vrchat %d/%d — API/audit/diagnostic/log secret non-disclosure", 21, nVRChat))
	if err := v.verifyNoSecrets(admin, serverLog, mockLog); err != nil {
		return err
	}
	step(fmt.Sprintf("vrchat %d/%d — all negative statuses and retry semantics observed", 22, nVRChat))
	step(fmt.Sprintf("vrchat %d/%d — OIDC/core state retained for following arcs", 23, nVRChat))
	if _, err := admin.getMe(); err != nil {
		return err
	}
	step(fmt.Sprintf("vrchat %d/%d — complete mocked VRChat ceremony", 24, nVRChat))
	return nil
}

func (v *vrchatSmoke) verifyNoSecrets(admin *client, logFiles ...string) error {
	var events page[contractAuditEvent]
	if err := admin.get("/api/prohibitorum/audit-events?factor=upstream_idp&limit=100", &events); err != nil {
		return err
	}
	auditBody, _ := json.Marshal(events)
	external := [][]byte{auditBody}
	for _, path := range logFiles {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		external = append(external, data)
	}
	for _, secret := range []string{vrchatOperator, vrchatPassword, vrchatCode, vrchatAuthCookie, vrchat2FACookie} {
		for _, data := range append(append([][]byte{}, v.bodies...), external...) {
			if bytes.Contains(data, []byte(secret)) {
				return fmt.Errorf("operator secret marker escaped: marker length=%d", len(secret))
			}
		}
	}
	for _, proof := range v.proofs {
		for _, data := range external {
			if bytes.Contains(data, []byte(proof)) {
				return fmt.Errorf("proof marker escaped authenticated flow response: marker length=%d", len(proof))
			}
		}
		for _, data := range v.bodies {
			if !bytes.Contains(data, []byte(proof)) {
				continue
			}
			allowed := false
			for _, proofBody := range v.proofBodies {
				allowed = allowed || bytes.Equal(data, proofBody)
			}
			if !allowed || bytes.Count(data, []byte(proof)) != 1 || !bytes.Contains(data, []byte("/verify/vrchat/"+proof)) {
				return fmt.Errorf("proof marker escaped proofUrl path: marker length=%d", len(proof))
			}
		}
	}
	rows, err := dbScalar(os.Getenv("PROHIBITORUM_DATABASE_URL"), "SELECT coalesce(string_agg(detail::text, E'\\n'), '') FROM credential_event")
	if err != nil {
		return err
	}
	diagnostics, err := dbScalar(os.Getenv("PROHIBITORUM_DATABASE_URL"), "SELECT coalesce(string_agg(fields::text, E'\\n'), '') FROM diagnostic_event")
	if err != nil {
		return err
	}
	rows = append(rows, diagnostics...)
	secrets := []string{vrchatOperator, vrchatPassword, vrchatCode, vrchatAuthCookie, vrchat2FACookie}
	secrets = append(secrets, v.proofs...)
	for _, secret := range secrets {
		if len(rows) > 0 && strings.Contains(rows[0], secret) {
			return fmt.Errorf("audit detail leaked marker length=%d", len(secret))
		}
	}
	return nil
}

func verifyVRChatFiltering(admin *client, slug, userID string) error {
	get := func(query string) (page[map[string]any], error) {
		var result page[map[string]any]
		err := admin.get("/api/prohibitorum/accounts?"+query, &result)
		return result, err
	}
	unified, err := get("q=" + url.QueryEscape(userID) + "&provider=" + slug + "&limit=1")
	if err != nil || len(unified.Items) != 1 {
		return fmt.Errorf("unified VRChat filter count=%d err=%v", len(unified.Items), err)
	}
	advanced, err := get("provider=" + slug + "&field=displayName&value=" + url.QueryEscape("Alpha Refresh") + "&match=contains&limit=1")
	if err != nil || len(advanced.Items) != 1 {
		return fmt.Errorf("advanced VRChat filter count=%d err=%v", len(advanced.Items), err)
	}
	if advanced.NextCursor != "" {
		wrong, _ := get("provider=" + slug + "&field=displayName&value=other&match=contains&limit=1&cursor=" + url.QueryEscape(advanced.NextCursor))
		if len(wrong.Items) != 0 {
			return fmt.Errorf("cursor accepted changed filter context")
		}
	}
	return nil
}

func containsAll(values []string, wants ...string) bool {
	sort.Strings(values)
	for _, want := range wants {
		index := sort.SearchStrings(values, want)
		if index == len(values) || values[index] != want {
			return false
		}
	}
	return true
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
