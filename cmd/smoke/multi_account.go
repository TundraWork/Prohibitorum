package main

// multi-account arc (PHB-285): two accounts signed in through one cookie jar,
// header selection, the OIDC account chooser, and signing out one account.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const nMulti = 7

type browserAccountsResponse struct {
	Limit    int `json:"limit"`
	Accounts []struct {
		AccountID int32  `json:"accountId"`
		U         string `json:"u"`
		Username  string `json:"username"`
	} `json:"accounts"`
}

// getAs sends GET path naming account in X-Prohibitorum-Account (0: no
// header) and decodes the JSON body into out. It returns the status and the
// response's X-Prohibitorum-Account.
func (c *client) getAs(path string, account int32, out any) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, "", err
	}
	if account != 0 {
		req.Header.Set("X-Prohibitorum-Account", strconv.Itoa(int(account)))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, "", err
		}
	}
	return resp.StatusCode, resp.Header.Get("X-Prohibitorum-Account"), nil
}

func (c *client) postAs(path string, account int32) (int, error) {
	req, err := http.NewRequest(http.MethodPost, c.base+path, nil)
	if err != nil {
		return 0, err
	}
	if account != 0 {
		req.Header.Set("X-Prohibitorum-Account", strconv.Itoa(int(account)))
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func signInWithPasskey(c *client, a *authenticator, base string) error {
	opts, err := c.beginLogin()
	if err != nil {
		return err
	}
	sig, err := a.signAssertion(opts.Challenge, base)
	if err != nil {
		return err
	}
	return c.completeLogin(a, sig)
}

// runMultiAccountArc signs admin and a second account in on one fresh jar and
// drives what only a multi-account browser does. admin is any signed-in admin
// client (used to invite the second account).
func runMultiAccountArc(admin *client, base string, adminAuth *authenticator, adminMe *meResponse, rpClientID, rpSecret, rpRedirectURI, issuer string) {
	step(fmt.Sprintf("multi %d/%d — two accounts sign in on one browser", 1, nMulti))
	_, secondAuth, secondMe, err := invitedPasskeyAccount(admin, base, "smoke-second", "Smoke Second")
	if err != nil {
		log.Fatalf("multi: second account: %v", err)
	}
	m, err := newClient(base)
	if err != nil {
		log.Fatalf("multi: client: %v", err)
	}
	if err := signInWithPasskey(m, adminAuth, base); err != nil {
		log.Fatalf("multi: admin sign-in: %v", err)
	}
	if err := signInWithPasskey(m, secondAuth, base); err != nil {
		log.Fatalf("multi: second sign-in: %v", err)
	}

	step(fmt.Sprintf("multi %d/%d — GET /auth/accounts lists both, in sign-in order", 2, nMulti))
	var list browserAccountsResponse
	if err := m.get("/api/prohibitorum/auth/accounts", &list); err != nil {
		log.Fatalf("multi: /auth/accounts: %v", err)
	}
	if len(list.Accounts) != 2 || list.Accounts[0].AccountID != adminMe.ID || list.Accounts[1].AccountID != secondMe.ID {
		log.Fatalf("multi: /auth/accounts = %+v, want [%d %d]", list.Accounts, adminMe.ID, secondMe.ID)
	}
	if list.Limit != 10 || len(list.Accounts[0].U) < 4 || list.Accounts[0].U == list.Accounts[1].U {
		log.Fatalf("multi: /auth/accounts limit/u = %d %q %q", list.Limit, list.Accounts[0].U, list.Accounts[1].U)
	}
	log.Printf("  [%s %s] limit=%d ✓", list.Accounts[0].Username, list.Accounts[1].Username, list.Limit)

	step(fmt.Sprintf("multi %d/%d — /me without a header is the first account; with one, the named account", 3, nMulti))
	var me meResponse
	if status, hdr, err := m.getAs("/api/prohibitorum/me", 0, &me); err != nil || status != 200 || me.ID != adminMe.ID || hdr != strconv.Itoa(int(adminMe.ID)) {
		log.Fatalf("multi: /me default = %d %+v header %q (%v)", status, me, hdr, err)
	}
	me = meResponse{}
	if status, hdr, err := m.getAs("/api/prohibitorum/me", secondMe.ID, &me); err != nil || status != 200 || me.ID != secondMe.ID || hdr != strconv.Itoa(int(secondMe.ID)) {
		log.Fatalf("multi: /me as second = %d %+v header %q (%v)", status, me, hdr, err)
	}
	if status, _, err := m.getAs("/api/prohibitorum/me", 999999, nil); err != nil || status != http.StatusUnauthorized {
		log.Fatalf("multi: /me for an account not here = %d (%v), want 401", status, err)
	}
	log.Printf("  default %s, header picks %s, unknown account 401 ✓", adminMe.Username, secondMe.Username)

	verifier, challenge := genPKCE()
	state := randState()
	authz := fmt.Sprintf(
		"/oauth/authorize?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		url.QueryEscape(rpClientID), url.QueryEscape(rpRedirectURI), url.QueryEscape("openid profile"),
		url.QueryEscape(state), url.QueryEscape(randState()), url.QueryEscape(challenge))

	step(fmt.Sprintf("multi %d/%d — prompt=none with two accounts → account_selection_required", 4, nMulti))
	loc, err := authorizeRaw(m, authz+"&prompt=none")
	if err != nil {
		log.Fatalf("multi: prompt=none: %v", err)
	}
	if lu, _ := url.Parse(loc); !strings.HasPrefix(loc, rpRedirectURI) || lu.Query().Get("error") != "account_selection_required" {
		log.Fatalf("multi: prompt=none Location = %q", loc)
	}
	log.Printf("  error=account_selection_required ✓")

	step(fmt.Sprintf("multi %d/%d — authorize → chooser → pick the second account → code for it", 5, nMulti))
	loc, err = authorizeRaw(m, authz)
	if err != nil {
		log.Fatalf("multi: authorize: %v", err)
	}
	cu, err := url.Parse(loc)
	if err != nil || !strings.HasSuffix(cu.Path, "/choose-account") || cu.Query().Get("ticket") == "" {
		log.Fatalf("multi: authorize should go to the chooser, got %q", loc)
	}
	ticket := cu.Query().Get("ticket")
	var chooser struct {
		App struct {
			Key string `json:"key"`
		} `json:"app"`
		Accounts []struct {
			AccountID int32 `json:"accountId"`
			Allowed   bool  `json:"allowed"`
		} `json:"accounts"`
	}
	if err := m.get("/api/prohibitorum/account-chooser?ticket="+url.QueryEscape(ticket), &chooser); err != nil {
		log.Fatalf("multi: GET /account-chooser: %v", err)
	}
	if chooser.App.Key != "oidc:"+rpClientID || len(chooser.Accounts) != 2 || !chooser.Accounts[1].Allowed {
		log.Fatalf("multi: chooser = %+v", chooser)
	}
	var chosen struct {
		Redirect string `json:"redirect"`
	}
	if err := m.postJSON("/api/prohibitorum/account-chooser", map[string]any{"ticket": ticket, "accountId": secondMe.ID}, &chosen); err != nil {
		log.Fatalf("multi: POST /account-chooser: %v", err)
	}
	resume, err := pathQueryOf(absolute(base, chosen.Redirect))
	if err != nil {
		log.Fatalf("multi: chooser redirect %q: %v", chosen.Redirect, err)
	}
	loc, err = authorizeRaw(m, resume)
	if err != nil {
		log.Fatalf("multi: resume authorize: %v", err)
	}
	code, err := parseAuthorizeRedirect(loc, rpRedirectURI, state, issuer)
	if err != nil {
		log.Fatalf("multi: resume authorize redirect: %v", err)
	}
	tok, err := tokenExchange(base, rpClientID, rpSecret, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {rpRedirectURI}, "code_verifier": {verifier},
	})
	if err != nil {
		log.Fatalf("multi: token: %v", err)
	}
	claims, err := verifyIDToken(base, tok.IDToken)
	if err != nil {
		log.Fatalf("multi: id_token: %v", err)
	}
	var second struct {
		OIDCSubject string `json:"oidcSubject"`
	}
	if err := admin.get(fmt.Sprintf("/api/prohibitorum/accounts/%d", secondMe.ID), &second); err != nil {
		log.Fatalf("multi: second account subject: %v", err)
	}
	if str(claims["sub"]) != second.OIDCSubject {
		log.Fatalf("multi: id_token sub = %q, want the second account's %q", str(claims["sub"]), second.OIDCSubject)
	}
	log.Printf("  id_token sub is the chosen account ✓")

	step(fmt.Sprintf("multi %d/%d — signing out the second account leaves the first", 6, nMulti))
	if status, err := m.postAs("/api/prohibitorum/auth/logout", secondMe.ID); err != nil || status != http.StatusNoContent {
		log.Fatalf("multi: logout second = %d (%v)", status, err)
	}
	list = browserAccountsResponse{}
	if err := m.get("/api/prohibitorum/auth/accounts", &list); err != nil || len(list.Accounts) != 1 || list.Accounts[0].AccountID != adminMe.ID {
		log.Fatalf("multi: after logout /auth/accounts = %+v (%v)", list.Accounts, err)
	}
	me = meResponse{}
	if status, _, err := m.getAs("/api/prohibitorum/me", 0, &me); err != nil || status != 200 || me.ID != adminMe.ID {
		log.Fatalf("multi: /me after logout = %d %+v (%v)", status, me, err)
	}
	log.Printf("  only %s remains ✓", adminMe.Username)

	step(fmt.Sprintf("multi %d/%d — logout-all clears the browser", 7, nMulti))
	if status, err := m.postAs("/api/prohibitorum/auth/logout-all", 0); err != nil || status != http.StatusNoContent {
		log.Fatalf("multi: logout-all = %d (%v)", status, err)
	}
	if hasSessionCookie(m.cookies()) {
		log.Fatalf("multi: session cookies left after logout-all: %v", m.cookies())
	}
	if status, _, _ := m.getAs("/api/prohibitorum/me", 0, nil); status != http.StatusUnauthorized {
		log.Fatalf("multi: /me after logout-all = %d, want 401", status)
	}
	log.Printf("  no session left ✓")
}

func absolute(base, ref string) string {
	if strings.HasPrefix(ref, "/") {
		return strings.TrimRight(base, "/") + ref
	}
	return ref
}
