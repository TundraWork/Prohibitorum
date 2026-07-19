package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
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
	vrchatUserC      = "usr_10000000-0000-0000-0000-000000000003"
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

type vrchatEnrollmentPreview struct {
	Intent               string          `json:"intent"`
	Target               json.RawMessage `json:"target,omitempty"`
	ExpiresAt            time.Time       `json:"expiresAt"`
	SuggestedDisplayName string          `json:"suggestedDisplayName,omitempty"`
}

type vrchatSmoke struct {
	base, control string
	controlClient *http.Client
	fixture       vrchatFixture
	bodies        [][]byte
	proofs        []string
	proofBodies   [][]byte
	enrollments   []string
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
	if token, err := enrollmentTokenFromCompletion(data); err == nil {
		v.enrollments = append(v.enrollments, token)
	}
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

func (v *vrchatSmoke) preview(c *client, token string) (vrchatEnrollmentPreview, map[string]json.RawMessage, error) {
	const label = "enrollment preview"
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/prohibitorum/enrollments/"+url.PathEscape(token), nil)
	if err != nil {
		return vrchatEnrollmentPreview{}, nil, fmt.Errorf("%s: request construction failed", label)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return vrchatEnrollmentPreview{}, nil, fmt.Errorf("%s: transport failed", label)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return vrchatEnrollmentPreview{}, nil, fmt.Errorf("%s: response read failed", label)
	}
	v.bodies = append(v.bodies, append([]byte(nil), body...))
	if resp.StatusCode != http.StatusOK {
		return vrchatEnrollmentPreview{}, nil, fmt.Errorf("%s: unexpected HTTP status %d", label, resp.StatusCode)
	}
	var (
		preview vrchatEnrollmentPreview
		fields  map[string]json.RawMessage
	)
	if err := json.Unmarshal(body, &preview); err != nil {
		return preview, nil, fmt.Errorf("%s: invalid JSON response", label)
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return preview, nil, fmt.Errorf("%s: invalid JSON response", label)
	}
	return preview, fields, nil
}

func enrollmentTokenFromCompletion(body []byte) (string, error) {
	var completion struct {
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return "", err
	}
	const prefix = "/enroll/"
	if !strings.HasPrefix(completion.Redirect, prefix) {
		return "", fmt.Errorf("completion did not select enrollment")
	}
	token, err := url.PathUnescape(strings.TrimPrefix(completion.Redirect, prefix))
	if err != nil || token == "" || strings.Contains(token, "/") {
		return "", fmt.Errorf("completion returned invalid enrollment destination")
	}
	return token, nil
}

func hasSessionCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		if strings.TrimPrefix(cookie.Name, "__Host-") == "prohibitorum_session" {
			return true
		}
	}
	return false
}

func getStatus(c *client, path string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func loginWithAuthenticatorStatus(c *client, auth *authenticator, origin string) (int, error) {
	assertion, err := c.beginLogin()
	if err != nil {
		return 0, err
	}
	signed, err := auth.signAssertion(assertion.Challenge, origin)
	if err != nil {
		return 0, err
	}
	credentialID := base64.RawURLEncoding.EncodeToString(auth.credentialID)
	userHandle := ""
	if signed.userHandle != nil {
		userHandle = base64.RawURLEncoding.EncodeToString(signed.userHandle)
	}
	resp, err := c.postJSONRaw("/api/prohibitorum/auth/login/complete", map[string]any{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(signed.authenticatorData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(signed.clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(signed.signature),
			"userHandle":        userHandle,
		},
	})
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func currentSessionID(c *client) (string, error) {
	sessions, err := c.listMySessions()
	if err != nil {
		return "", err
	}
	current := ""
	for _, session := range sessions {
		if !session.IsCurrent {
			continue
		}
		if current != "" {
			return "", fmt.Errorf("multiple current sessions")
		}
		current = session.ID
	}
	if current == "" {
		return "", fmt.Errorf("no current session")
	}
	return current, nil
}

func registerVRChatEnrollment(c *client, base, token, username, displayName, nickname string) (*authenticator, error) {
	creation, err := c.beginEnrollment(token, username, displayName, nickname)
	if err != nil {
		return nil, err
	}
	auth, err := newAuthenticator(creation.RP.ID)
	if err != nil {
		return nil, err
	}
	attestation, err := auth.attestCredential(creation.Challenge, creation.User.ID, base)
	if err != nil {
		return nil, err
	}
	if err := c.completeEnrollment(token, auth, attestation); err != nil {
		return nil, err
	}
	return auth, nil
}

func runVRChatSmoke(admin *client, base, control, caFile, serverLog, mockLog string) error {
	v, err := newVRChatSmoke(base, control, caFile)
	if err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — create disabled, unconfigured fixed link_only provider", 1, nVRChat))
	var provider vrchatProviderView
	if err := admin.postJSON("/api/prohibitorum/identity-providers", map[string]any{"slug": vrchatSlug, "displayName": "VRChat Smoke", "protocol": "vrchat", "mode": "link_only", "config": map[string]any{}}, &provider); err != nil {
		return err
	}
	if provider.Slug != vrchatSlug || provider.Protocol != "vrchat" || provider.Mode != "link_only" || !provider.Disabled || provider.Ready || provider.SecretConfigured || provider.SecretStatus != "unconfigured" {
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

	step(fmt.Sprintf("vrchat %d/%d — verify TOTP, validate ready, enable link_only provider", 3, nVRChat))
	var verified vrchatOperatorResult
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/verify", map[string]string{"challenge": started.Challenge, "method": "totp", "code": vrchatCode}, &verified); err != nil || verified.Status != "valid" {
		return fmt.Errorf("operator verify status=%q err=%w", verified.Status, err)
	}
	var validated vrchatOperatorResult
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/validate", nil, &validated); err != nil || validated.Provider == nil || !validated.Provider.Ready || validated.Provider.SecretStatus != "valid" {
		return fmt.Errorf("operator validate = %+v err=%w", validated, err)
	}
	provider = *validated.Provider
	if err := admin.postJSON("/api/prohibitorum/identity-providers/set-disabled", map[string]any{"slug": vrchatSlug, "disabled": false}, &provider); err != nil || provider.Disabled || !provider.Ready || provider.Mode != "link_only" {
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

	step(fmt.Sprintf("vrchat %d/%d — registration proof returns enrollment; no session cookie and /me stays 401", 5, nVRChat))
	registration, _ := newFederationClient(base)
	registrationFlow, err := v.begin(registration, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	if err != nil {
		return err
	}
	registrationView, err := v.prepare(registration, registrationFlow, vrchatUserA)
	if err != nil || registrationView.Step != "proof" || registrationView.RequiresLocalUsername {
		return fmt.Errorf("registration prepare = %+v err=%w", registrationView, err)
	}
	proofA, err := v.publish(registrationView)
	if err != nil {
		return err
	}
	reloaded, err := v.reload(registration, registrationFlow)
	if err != nil || reloaded.ProofURL != registrationView.ProofURL {
		return fmt.Errorf("prepared registration flow reload proof mismatch")
	}
	resp, body, err := v.verify(registration, registrationFlow, "")
	if err != nil || statusOf(resp) != http.StatusOK {
		return fmt.Errorf("registration proof status=%v err=%w", statusOf(resp), err)
	}
	registrationToken, err := enrollmentTokenFromCompletion(body)
	if err != nil {
		return err
	}
	if hasSessionCookie(resp.Cookies()) || hasSessionCookie(registration.cookies()) {
		return fmt.Errorf("registration proof set a normal session cookie")
	}
	if status, err := getStatus(registration, "/api/prohibitorum/me"); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("registration proof /me status=%d err=%w; want 401", status, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — registration preview is federated_register with display suggestion only", 6, nVRChat))
	registrationPreview, registrationFields, err := v.preview(registration, registrationToken)
	if err != nil {
		return err
	}
	if registrationPreview.Intent != "federated_register" || registrationPreview.SuggestedDisplayName != "VRChat Smoke Alpha" || len(registrationPreview.Target) != 0 || registrationPreview.ExpiresAt.IsZero() {
		return fmt.Errorf("registration preview shape = %+v", registrationPreview)
	}
	for field := range registrationFields {
		if field != "intent" && field != "expiresAt" && field != "suggestedDisplayName" {
			return fmt.Errorf("registration preview exposed unexpected field %q", field)
		}
	}

	step(fmt.Sprintf("vrchat %d/%d — registration proof replay is rejected without a session", 7, nVRChat))
	resp, body, _ = v.verify(registration, registrationFlow, "")
	if err := v.expectError(resp, body, http.StatusUnauthorized, "federation_state_invalid"); err != nil {
		return err
	}
	if hasSessionCookie(resp.Cookies()) || hasSessionCookie(registration.cookies()) {
		return fmt.Errorf("registration proof replay issued a normal session")
	}
	if status, err := getStatus(registration, "/api/prohibitorum/me"); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("registration proof replay /me status=%d err=%w; want 401", status, err)
	}
	if err := assertNoVRChatIdentity(admin, vrchatSlug, vrchatUserA); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — registration WebAuthn ceremony creates identity and first session", 8, nVRChat))
	registrationAuth, err := registerVRChatEnrollment(registration, base, registrationToken, "vrchat-smoke-alpha", "VRChat Smoke Alpha", "vrchat-primary")
	if err != nil {
		return err
	}
	if !hasSessionCookie(registration.cookies()) {
		return fmt.Errorf("registration completion did not establish a normal session")
	}
	registrationMe, err := registration.getMe()
	if err != nil || registrationMe.Username != "vrchat-smoke-alpha" || registrationMe.DisplayName != "VRChat Smoke Alpha" {
		return fmt.Errorf("registration session = %+v err=%w", registrationMe, err)
	}
	registrationSessions, err := registration.listMySessions()
	if err != nil || len(registrationSessions) != 1 || !registrationSessions[0].IsCurrent {
		return fmt.Errorf("registration sessions = %+v err=%w", registrationSessions, err)
	}
	oldSessionID := registrationSessions[0].ID
	identities, err := registration.listMyIdentities()
	if err != nil || len(identities) != 1 {
		return fmt.Errorf("registration identities = %+v err=%w", identities, err)
	}
	identity := identities[0]
	if identity.IdpSlug != vrchatSlug || identity.Protocol != "vrchat" || identity.Subject != vrchatUserA ||
		identity.Data["userId"] != vrchatUserA || identity.Data["displayName"] != "VRChat Smoke Alpha" ||
		identity.Data["profileUrl"] != "https://vrchat.com/home/user/"+vrchatUserA || len(identity.Data) != 3 {
		return fmt.Errorf("registration identity metadata projection is incomplete or uncurated")
	}

	step(fmt.Sprintf("vrchat %d/%d — recovery proof returns target-hidden reset and no session", 9, nVRChat))
	v.fixture.DisplayName = "VRChat Smoke Alpha Refreshed"
	v.fixture.AvatarURL = "https://api.vrchat.cloud/avatar-alpha-refreshed.png"
	v.fixture.BioLinks = nil
	if err := v.setFixture(); err != nil {
		return err
	}
	recovery, _ := newFederationClient(base)
	recoveryFlow, err := v.begin(recovery, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	if err != nil {
		return err
	}
	recoveryView, err := v.prepare(recovery, recoveryFlow, vrchatUserA)
	if err != nil || recoveryView.Step != "proof" || recoveryView.RequiresLocalUsername {
		return fmt.Errorf("recovery prepare = %+v err=%w", recoveryView, err)
	}
	proofRecovery, err := v.publish(recoveryView)
	if err != nil || proofRecovery == proofA {
		return fmt.Errorf("recovery proof was not fresh")
	}
	resp, body, err = v.verify(recovery, recoveryFlow, "")
	if err != nil || statusOf(resp) != http.StatusOK {
		return fmt.Errorf("recovery proof status=%v err=%w", statusOf(resp), err)
	}
	recoveryToken, err := enrollmentTokenFromCompletion(body)
	if err != nil {
		return err
	}
	if hasSessionCookie(resp.Cookies()) || hasSessionCookie(recovery.cookies()) {
		return fmt.Errorf("recovery proof set a normal session cookie")
	}
	if status, err := getStatus(recovery, "/api/prohibitorum/me"); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("recovery proof /me status=%d err=%w; want 401", status, err)
	}
	recoveryPreview, recoveryFields, err := v.preview(recovery, recoveryToken)
	if err != nil {
		return err
	}
	if recoveryPreview.Intent != "reset" || len(recoveryPreview.Target) != 0 || recoveryPreview.SuggestedDisplayName != "" || recoveryPreview.ExpiresAt.IsZero() {
		return fmt.Errorf("provider-backed reset preview was not target-hidden")
	}
	for field := range recoveryFields {
		if field != "intent" && field != "expiresAt" {
			return fmt.Errorf("provider-backed reset preview exposed unexpected field %q", field)
		}
	}

	step(fmt.Sprintf("vrchat %d/%d — recovery replacement revokes old session and issues exactly one fresh session", 10, nVRChat))
	if status, err := getStatus(registration, "/api/prohibitorum/me"); err != nil || status != http.StatusOK {
		return fmt.Errorf("proof-only recovery changed original session /me status=%d err=%w; want 200", status, err)
	}
	if _, err := registerVRChatEnrollment(recovery, base, recoveryToken, "", "", "vrchat-replacement"); err != nil {
		return err
	}
	if status, err := getStatus(registration, "/api/prohibitorum/me"); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("pre-recovery session /me status=%d err=%w; want 401", status, err)
	}
	recoveryMe, err := recovery.getMe()
	if err != nil || recoveryMe.ID != registrationMe.ID {
		return fmt.Errorf("replacement session = %+v err=%w", recoveryMe, err)
	}
	recoverySessions, err := recovery.listMySessions()
	if err != nil || len(recoverySessions) != 1 || !recoverySessions[0].IsCurrent || recoverySessions[0].ID == oldSessionID {
		return fmt.Errorf("replacement sessions did not contain exactly one fresh current session")
	}
	replacementCredentials, err := recovery.listMyCredentials()
	if err != nil || len(replacementCredentials) != 1 || replacementCredentials[0].Nickname != "vrchat-replacement" {
		return fmt.Errorf("replacement credential list = %+v err=%w", replacementCredentials, err)
	}
	oldCredentialClient, _ := newClient(base)
	if status, err := loginWithAuthenticatorStatus(oldCredentialClient, registrationAuth, base); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("replaced credential login status=%d err=%w; want 401", status, err)
	}
	if hasSessionCookie(oldCredentialClient.cookies()) {
		return fmt.Errorf("replaced credential login issued a session")
	}

	step(fmt.Sprintf("vrchat %d/%d — wrong public user is rejected safely", 11, nVRChat))
	wrong, _ := newFederationClient(base)
	wrongFlow, _ := v.begin(wrong, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	wrongView, _ := v.prepare(wrong, wrongFlow, vrchatUserA)
	_, _ = v.publish(wrongView)
	v.fixture.PublicUserID = vrchatWrongUser
	_ = v.setFixture()
	resp, body, _ = v.verify(wrong, wrongFlow, "")
	if err := v.expectError(resp, body, http.StatusBadRequest, "vrchat_identity_invalid"); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — missing proof retries in the same flow", 12, nVRChat))
	v.fixture.PublicUserID, v.fixture.BioLinks = vrchatUserA, nil
	_ = v.setFixture()
	missing, _ := newFederationClient(base)
	missingFlow, _ := v.begin(missing, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	missingView, _ := v.prepare(missing, missingFlow, vrchatUserA)
	resp, body, _ = v.verify(missing, missingFlow, "")
	if err := v.expectError(resp, body, http.StatusConflict, "vrchat_proof_missing"); err != nil {
		return err
	}
	_, _ = v.publish(missingView)
	resp, _, _ = v.verify(missing, missingFlow, "")
	if statusOf(resp) != http.StatusOK || hasSessionCookie(resp.Cookies()) {
		return fmt.Errorf("same-flow proof retry status=%d or issued session", statusOf(resp))
	}

	step(fmt.Sprintf("vrchat %d/%d — browser cookie swap and expired state are rejected", 13, nVRChat))
	swapOwner, _ := newFederationClient(base)
	swapFlow, _ := v.begin(swapOwner, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	swapOther, _ := newFederationClient(base)
	if _, err := v.reload(swapOther, swapFlow); err == nil || !strings.Contains(err.Error(), "status=401") {
		return fmt.Errorf("browser swap error=%v", err)
	}
	expired, _ := newFederationClient(base)
	expiredFlow, _ := v.begin(expired, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	time.Sleep(6 * time.Second)
	if _, err := v.reload(expired, expiredFlow); err == nil || !strings.Contains(err.Error(), "status=401") || !strings.Contains(err.Error(), "federation_state_invalid") {
		return fmt.Errorf("expiry error=%v", err)
	}

	step(fmt.Sprintf("vrchat %d/%d — 429 Retry-After recovers in the same flow", 14, nVRChat))
	rate, _ := newFederationClient(base)
	rateFlow, _ := v.begin(rate, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	rateView, _ := v.prepare(rate, rateFlow, vrchatUserA)
	_, _ = v.publish(rateView)
	v.fixture.PublicStatus, v.fixture.RetryAfter = http.StatusTooManyRequests, "1"
	_ = v.setFixture()
	resp, body, _ = v.verify(rate, rateFlow, "")
	if err := v.expectError(resp, body, http.StatusTooManyRequests, "upstream_rate_limited"); err != nil || resp.Header.Get("Retry-After") != "1" {
		return fmt.Errorf("rate limit: %v header=%q", err, resp.Header.Get("Retry-After"))
	}
	time.Sleep(1100 * time.Millisecond)
	v.fixture.PublicStatus, v.fixture.RetryAfter = 0, ""
	_ = v.setFixture()
	resp, _, _ = v.verify(rate, rateFlow, "")
	if statusOf(resp) != http.StatusOK || hasSessionCookie(resp.Cookies()) {
		return fmt.Errorf("rate recovery status=%d or issued session", statusOf(resp))
	}

	step(fmt.Sprintf("vrchat %d/%d — malformed, oversized, and temporary 5xx responses remain retryable", 15, nVRChat))
	for _, failure := range []struct {
		mode   string
		status int
	}{{"malformed", 0}, {"oversized", 0}, {"", http.StatusServiceUnavailable}} {
		failureClient, _ := newFederationClient(base)
		failureFlow, _ := v.begin(failureClient, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
		failureView, _ := v.prepare(failureClient, failureFlow, vrchatUserA)
		_, _ = v.publish(failureView)
		v.fixture.PublicBodyMode, v.fixture.PublicStatus = failure.mode, failure.status
		_ = v.setFixture()
		resp, body, _ = v.verify(failureClient, failureFlow, "")
		if err := v.expectError(resp, body, http.StatusServiceUnavailable, "upstream_temporarily_unavailable"); err != nil {
			return fmt.Errorf("public failure %q/%d: %w", failure.mode, failure.status, err)
		}
		v.fixture.PublicBodyMode, v.fixture.PublicStatus = "", 0
		_ = v.setFixture()
		resp, _, _ = v.verify(failureClient, failureFlow, "")
		if statusOf(resp) != http.StatusOK || hasSessionCookie(resp.Cookies()) {
			return fmt.Errorf("public failure recovery %q/%d status=%d or issued session", failure.mode, failure.status, statusOf(resp))
		}
	}

	step(fmt.Sprintf("vrchat %d/%d — operator 401 invalidates readiness, then deterministic setup recovers", 16, nVRChat))
	unauthorized, _ := newFederationClient(base)
	unauthorizedFlow, _ := v.begin(unauthorized, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	unauthorizedView, _ := v.prepare(unauthorized, unauthorizedFlow, vrchatUserA)
	_, _ = v.publish(unauthorizedView)
	v.fixture.PublicStatus = http.StatusUnauthorized
	_ = v.setFixture()
	resp, body, _ = v.verify(unauthorized, unauthorizedFlow, "")
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
	if err := admin.postJSON("/api/prohibitorum/identity-providers/"+vrchatSlug+"/operator-session/validate", nil, &validated); err != nil || validated.Provider == nil || !validated.Provider.Ready {
		return fmt.Errorf("operator recovery validate = %+v err=%w", validated, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — authenticated Connected Accounts link remains session-bound", 17, nVRChat))
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName, v.fixture.BioLinks = vrchatUserA, vrchatUserA, "VRChat Smoke Alpha Refreshed", nil
	_ = v.setFixture()
	conflictFlow, err := v.begin(admin, "/api/prohibitorum/me/identities/link/"+vrchatSlug+"/begin?return_to=/connected")
	if err != nil {
		return err
	}
	conflictView, _ := v.prepare(admin, conflictFlow, vrchatUserA)
	_, _ = v.publish(conflictView)
	resp, body, _ = v.verify(admin, conflictFlow, "")
	if err := v.expectError(resp, body, http.StatusConflict, "federation_identity_conflict"); err != nil {
		return err
	}
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName, v.fixture.BioLinks = vrchatUserC, vrchatUserC, "VRChat Smoke Linked", nil
	_ = v.setFixture()
	beforeLinkSession, err := currentSessionID(admin)
	if err != nil {
		return err
	}
	linkFlow, err := v.begin(admin, "/api/prohibitorum/me/identities/link/"+vrchatSlug+"/begin?return_to=/connected")
	if err != nil {
		return err
	}
	linkView, _ := v.prepare(admin, linkFlow, vrchatUserC)
	_, _ = v.publish(linkView)
	resp, body, _ = v.verify(admin, linkFlow, "")
	if statusOf(resp) != http.StatusOK || !bytes.Contains(body, []byte(`"redirect":"/connected"`)) {
		return fmt.Errorf("authenticated link status=%d", statusOf(resp))
	}
	afterLinkSession, err := currentSessionID(admin)
	if err != nil || afterLinkSession != beforeLinkSession {
		return fmt.Errorf("authenticated link changed the operator session")
	}

	step(fmt.Sprintf("vrchat %d/%d — disabled provider proof fails safely without session issuance", 18, nVRChat))
	v.fixture.CurrentUserID, v.fixture.PublicUserID, v.fixture.DisplayName, v.fixture.BioLinks = vrchatUserA, vrchatUserA, "VRChat Smoke Alpha Refreshed", nil
	_ = v.setFixture()
	disabledProviderClient, _ := newFederationClient(base)
	disabledProviderFlow, _ := v.begin(disabledProviderClient, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	disabledProviderView, _ := v.prepare(disabledProviderClient, disabledProviderFlow, vrchatUserA)
	_, _ = v.publish(disabledProviderView)
	if err := admin.postJSON("/api/prohibitorum/identity-providers/set-disabled", map[string]any{"slug": vrchatSlug, "disabled": true}, &provider); err != nil || !provider.Disabled {
		return fmt.Errorf("provider disable = %+v err=%w", provider, err)
	}
	resp, body, _ = v.verify(disabledProviderClient, disabledProviderFlow, "")
	if err := v.expectError(resp, body, http.StatusUnauthorized, "federation_state_invalid"); err != nil || hasSessionCookie(resp.Cookies()) {
		return fmt.Errorf("disabled provider proof: %w", err)
	}
	if err := admin.postJSON("/api/prohibitorum/identity-providers/set-disabled", map[string]any{"slug": vrchatSlug, "disabled": false}, &provider); err != nil || provider.Disabled {
		return fmt.Errorf("provider re-enable = %+v err=%w", provider, err)
	}

	step(fmt.Sprintf("vrchat %d/%d — disabled linked account proof stays opaque and sessionless", 19, nVRChat))
	disabledAccountClient, _ := newFederationClient(base)
	disabledAccountFlow, _ := v.begin(disabledAccountClient, "/api/prohibitorum/auth/federation/"+vrchatSlug+"/login?return_to=/me")
	disabledAccountView, _ := v.prepare(disabledAccountClient, disabledAccountFlow, vrchatUserA)
	_, _ = v.publish(disabledAccountView)
	if err := admin.postJSON("/api/prohibitorum/accounts/set-disabled", map[string]any{"id": registrationMe.ID, "disabled": true}, nil); err != nil {
		return err
	}
	resp, body, _ = v.verify(disabledAccountClient, disabledAccountFlow, "")
	if err := v.expectError(resp, body, http.StatusUnauthorized, "bad_credentials"); err != nil || hasSessionCookie(resp.Cookies()) {
		return fmt.Errorf("disabled account proof: %w", err)
	}
	if status, err := getStatus(recovery, "/api/prohibitorum/me"); err != nil || status != http.StatusUnauthorized {
		return fmt.Errorf("disabled-account replacement session /me status=%d err=%w; want 401", status, err)
	}
	if err := admin.postJSON("/api/prohibitorum/accounts/set-disabled", map[string]any{"id": registrationMe.ID, "disabled": false}, nil); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — curated identity metadata remains filterable", 20, nVRChat))
	if err := verifyVRChatFiltering(admin, vrchatSlug, vrchatUserA); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — public proof page is token-invariant and no-store", 21, nVRChat))
	var shell []byte
	for _, proof := range []string{proofA, proofRecovery, "invalid-public-proof"} {
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

	step(fmt.Sprintf("vrchat %d/%d — API, audit, diagnostics, and logs omit proof/operator secrets", 22, nVRChat))
	if err := v.verifyNoSecrets(admin, serverLog, mockLog); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — registration, recovery, link, replay, and disabled paths observed", 23, nVRChat))
	if _, err := admin.getMe(); err != nil {
		return err
	}

	step(fmt.Sprintf("vrchat %d/%d — complete proof-backed registration and recovery lifecycle", 24, nVRChat))
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
	for _, token := range v.enrollments {
		for _, data := range external {
			if bytes.Contains(data, []byte(token)) {
				return fmt.Errorf("enrollment marker escaped to external output: marker length=%d", len(token))
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
	secrets = append(secrets, v.enrollments...)
	for _, secret := range secrets {
		for _, row := range rows {
			if strings.Contains(row, secret) {
				return fmt.Errorf("audit or diagnostic detail leaked marker length=%d", len(secret))
			}
		}
	}
	return nil
}

func assertNoVRChatIdentity(admin *client, slug, userID string) error {
	var result page[map[string]any]
	query := "q=" + url.QueryEscape(userID) + "&provider=" + url.QueryEscape(slug) + "&limit=1"
	if err := admin.get("/api/prohibitorum/accounts?"+query, &result); err != nil {
		return fmt.Errorf("query pre-enrollment VRChat identity: %w", err)
	}
	if len(result.Items) != 0 {
		return fmt.Errorf("proof eagerly materialized VRChat provider/subject identity")
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
	advanced, err := get("provider=" + slug + "&field=displayName&value=" + url.QueryEscape("Alpha") + "&match=contains&limit=1")
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
