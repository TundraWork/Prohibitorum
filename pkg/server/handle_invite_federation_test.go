// Package server — handle_invite_federation_test.go
//
// Handler-level tests for the invite-bound federation entrypoint
// (GET /enrollments/{token}/start-federation).
//
// Scaffolding decision: extend the fedTestHarness from handle_federation_test.go
// rather than build a new one. The two flows share every collaborator
// (mock OP, fake querier, KV, audit, sessionStore) and the only new surface
// is enrollment seeding + the start-federation handler. The existing
// fakeFedQueries embeds db.Querier, so we add GetEnrollmentByToken +
// ConsumeEnrollment as new methods on the same fake (Go method sets
// span files within a package).

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation"
	sessstore "prohibitorum/pkg/session"
)

// --- fake extensions for enrollment lookup / consume ----------------------
//
// These methods extend the fakeFedQueries declared in handle_federation_test.go.
// Tokens are stored in a single map: GetEnrollmentByToken reads it for the
// /start-federation validation, ConsumeEnrollment mutates ConsumedAt + returns
// the row only if it was unconsumed AND unexpired (mirrors the SQL CTE).

func (f *fakeFedQueries) seedEnrollment(enr db.Enrollment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enrollmentByToken == nil {
		f.enrollmentByToken = map[string]db.Enrollment{}
	}
	f.enrollmentByToken[enr.Token] = enr
}

func (f *fakeFedQueries) GetEnrollmentByToken(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.enrollmentByToken[token]; ok {
		return e, nil
	}
	return db.Enrollment{}, pgx.ErrNoRows
}

func (f *fakeFedQueries) ConsumeEnrollment(_ context.Context, token string) (db.Enrollment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.enrollmentByToken[token]
	if !ok {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	// Mirror the UPDATE ... WHERE consumed_at IS NULL AND expires_at > now()
	// semantics — caller sees pgx.ErrNoRows for any "not redeemable" branch.
	if e.ConsumedAt.Valid {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	if !e.ExpiresAt.Valid || !e.ExpiresAt.Time.After(time.Now()) {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	e.ConsumedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	f.enrollmentByToken[token] = e
	return e, nil
}

// ConsumeInviteEnrollment delegates to ConsumeEnrollment here — the real query's
// intent='invite' restriction is verified against PG by the smoke's invite_only
// federation arc, not by this fake (audit OIDCFED-2).
func (f *fakeFedQueries) ConsumeInviteEnrollment(ctx context.Context, token string) (db.Enrollment, error) {
	return f.ConsumeEnrollment(ctx, token)
}

// --- helpers --------------------------------------------------------------

// driveStartFederation hits /enrollments/{token}/start-federation and returns
// (authorizeURL, response). Empty location on non-302. selectedSlug is the
// invitee-chosen provider query parameter ("" omits it).
func (h *fedTestHarness) driveStartFederation(t *testing.T, token, returnTo string, selectedSlug ...string) (string, *http.Response) {
	t.Helper()
	q := url.Values{}
	if returnTo != "" {
		q.Set("return_to", returnTo)
	}
	if len(selectedSlug) > 0 && selectedSlug[0] != "" {
		q.Set("provider", selectedSlug[0])
	}
	u := h.srvTS.URL + "/api/prohibitorum/enrollments/" + token + "/start-federation"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	resp, err := h.client.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusFound {
		return "", resp
	}
	return resp.Header.Get("Location"), resp
}

// newInviteTestServer wraps newFederationTestServer, then additionally
// mounts the /start-federation route under the same chi router. The base
// harness only mounts /login + /callback — we need start-federation too.
func newInviteTestServer(t *testing.T) *fedTestHarness {
	t.Helper()
	h := newFederationTestServer(t)
	// Re-mount with start-federation added. The original chi.Router is owned
	// by httptest.Server, so we replace srvTS to swap the handler.
	r := chi.NewRouter()
	r.Get("/api/prohibitorum/auth/federation/{slug}/login", h.s.handleFederationLoginHTTP)
	r.Get("/api/prohibitorum/auth/federation/{slug}/callback", h.s.handleFederationCallbackHTTP)
	r.Get("/api/prohibitorum/enrollments/{token}/start-federation", h.s.handleEnrollmentStartFederationHTTP)
	r.Get("/api/prohibitorum/auth/federation/confirm", h.s.handleFederationConfirmGet)
	r.Post("/api/prohibitorum/auth/federation/confirm", h.s.handleFederationConfirmPost)
	r.Post("/api/prohibitorum/auth/federation/confirm/decline", h.s.handleFederationConfirmDecline)
	h.srvTS.Config.Handler = r
	return h
}

// futureExp returns a pgtype.Timestamptz an hour in the future.
func futureExp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
}

// pastExp returns a pgtype.Timestamptz an hour in the past.
func pastExp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
}

// validInvite builds a redeemable enrollment row bound to the given slug.
// Provisioning takes the username/display name from the upstream claims; the
// template only carries role + attributes.
func validInvite(token, slug string) db.Enrollment {
	return db.Enrollment{
		Token:                   token,
		Intent:                  "invite",
		ExpectedUpstreamIdpSlug: pgtype.Text{String: slug, Valid: true},
		TemplateRole:            pgtype.Text{String: "user", Valid: true},
		TemplateAttributes:      []byte("{}"),
		ExpiresAt:               futureExp(),
	}
}

// --- tests ----------------------------------------------------------------

func TestEnrollmentStartFederation_HappyPath(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-happy", h.idp.Slug))

	loc, resp := h.driveStartFederation(t, "tok-happy", "/me")
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("status: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if !strings.HasPrefix(loc, h.opTS.URL+"/authorize") {
		t.Errorf("Location: want prefix %q, got %q", h.opTS.URL+"/authorize", loc)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy: want no-referrer, got %q", got)
	}

	// State must live under LoginKey with EnrollmentToken set.
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state missing from authorize URL")
	}
	blob, err := h.s.kvStore.Get(context.Background(), fedoidc.FlowKey(state))
	if err != nil {
		t.Fatalf("state not stashed under LoginKey: %v", err)
	}
	fs, err := fedoidc.DecodeFlowState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if fs.EnrollmentToken != "tok-happy" {
		t.Errorf("FedState.EnrollmentToken = %q, want tok-happy", fs.EnrollmentToken)
	}
	if fs.LinkAccountID != nil {
		t.Errorf("LinkAccountID = %v, want nil (invite flow has no account yet)", *fs.LinkAccountID)
	}
	if fs.ReturnTo != "/me" {
		t.Errorf("ReturnTo = %q, want /me", fs.ReturnTo)
	}
}

func TestEnrollmentStartFederation_UnknownToken(t *testing.T) {
	h := newInviteTestServer(t)
	// No enrollment seeded.

	_, resp := h.driveStartFederation(t, "no-such-token", "/me")
	// Browser-navigated error path now redirects to SPA /error page.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_ConsumedToken(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-consumed", h.idp.Slug)
	enr.ConsumedAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-consumed", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_ExpiredToken(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-expired", h.idp.Slug)
	enr.ExpiresAt = pastExp()
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-expired", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_NonFederationIntent(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-bootstrap", h.idp.Slug)
	enr.Intent = "bootstrap" // Even with slug binding, non-invite intent must reject.
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-bootstrap", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_NoSlugBinding(t *testing.T) {
	h := newInviteTestServer(t)
	enr := validInvite("tok-no-slug", h.idp.Slug)
	enr.ExpectedUpstreamIdpSlug = pgtype.Text{Valid: false} // NULL — WebAuthn invite, not federation.
	h.q.seedEnrollment(enr)

	_, resp := h.driveStartFederation(t, "tok-no-slug", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_InvalidReturnTo(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-rt", h.idp.Slug))

	_, resp := h.driveStartFederation(t, "tok-rt", "https://evil.example.com")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invalid_return_to&ref=") {
		t.Errorf("Location: want /error?error=invalid_return_to&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_FullFlow_RedeemsInvite(t *testing.T) {
	h := newInviteTestServer(t)
	// Mock OP is in ModeAutoProvision per harness defaults — the
	// EnrollmentToken on FedState is what routes the callback into the
	// invite+provider provisioning path. The username/display name come from
	// the upstream claims ("alice"/"Alice Example"); the template only
	// carries role + attributes.
	h.q.seedEnrollment(validInvite("tok-full", h.idp.Slug))

	// Step 1: /start-federation → 302 to /authorize.
	loc, resp := h.driveStartFederation(t, "tok-full", "/me")
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("start-federation: want 302, got %d (body=%s)", resp.StatusCode, body)
	}

	// Step 2: follow upstream /authorize → 302 to /callback with code+state.
	code, state, iss := driveAuthorize(t, loc)

	// Step 3: hit /callback → 302 to /welcome with a fed-state confirmation
	// grant cookie. No session yet — the identity is unconfirmed.
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitCallback(t, h.idp.Slug, q)
	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/welcome" {
		t.Errorf("Location: want /welcome, got %q", got)
	}
	var grantCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.FedStateCookieName && c.Value != "" {
			grantCookie = c
			break
		}
	}
	if grantCookie == nil {
		t.Fatal("confirmation grant cookie not set after invite provisioning")
	}

	// Enrollment must be consumed.
	enr, err := h.q.GetEnrollmentByToken(context.Background(), "tok-full")
	if err != nil {
		t.Fatalf("post-flow GetEnrollmentByToken: %v", err)
	}
	if !enr.ConsumedAt.Valid {
		t.Error("enrollment ConsumedAt: want set, got NULL")
	}

	// Exactly one account inserted, provisioned from the upstream claims and
	// the invite template role.
	if len(h.q.insertedAccounts) != 1 {
		t.Fatalf("accounts inserted: want 1, got %d", len(h.q.insertedAccounts))
	}
	acct := h.q.insertedAccounts[0]
	if acct.Username != "alice" {
		t.Errorf("account Username: want alice (from upstream preferred_username), got %q", acct.Username)
	}
	if acct.Role != "user" {
		t.Errorf("account Role: want user (from template), got %q", acct.Role)
	}

	// Identity row links to (upstream_iss, upstream_sub).
	if len(h.q.insertIdentitys) != 1 {
		t.Fatalf("identities inserted: want 1, got %d", len(h.q.insertIdentitys))
	}
	if h.q.insertIdentitys[0].UpstreamSub == "" {
		t.Error("identity UpstreamSub: want non-empty")
	}

	// Audit must show a federation_oidc register with reason=invite_provisioned.
	var found bool
	for _, ev := range h.q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) {
			continue
		}
		if ev.Event != audit.EventRegister {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(ev.Detail, &detail); err != nil {
			t.Fatalf("decode audit detail: %v", err)
		}
		if detail["reason"] == "invite_provisioned" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no audit register row with reason=invite_provisioned")
	}
}

func TestEnrollmentStartFederation_SteamFullHTTPFlow(t *testing.T) {
	h := newInviteTestServer(t)
	provider := seedSteamProvider(t, h)
	h.q.seedEnrollment(validInvite("tok-steam", provider.Slug))
	avatars := &serverAvatarRecorder{}
	h.s.federationService.SetAvatarManager(avatars)

	loc, resp := h.driveStartFederation(t, "tok-steam", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	actionURL, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	state := actionURL.Query().Get("state")
	q := url.Values{
		"state":       {state},
		"openid.mode": {"id_res"},
	}
	resp = h.hitCallback(t, provider.Slug, q)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/welcome" {
		t.Fatalf("callback status/location = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// The identity stays unconfirmed, so no session exists yet — the
	// confirmation grant routes the user through /welcome first.
	if len(h.q.sessions) != 0 {
		t.Fatalf("sessions inserted = %d, want 0 (identity unconfirmed)", len(h.q.sessions))
	}
	enrollment, err := h.q.GetEnrollmentByToken(context.Background(), "tok-steam")
	if err != nil || !enrollment.ConsumedAt.Valid {
		t.Fatalf("Steam enrollment was not consumed: enrollment=%+v err=%v", enrollment, err)
	}
	if len(h.q.insertedAccounts) != 1 || h.q.insertedAccounts[0].Username != "steam_76561198000000000" ||
		len(h.q.insertIdentitys) != 1 ||
		h.q.insertIdentitys[0].UpstreamIss != "https://steamcommunity.com/openid" ||
		h.q.insertIdentitys[0].UpstreamSub != "76561198000000000" {
		t.Fatalf("Steam invite provisioning: accounts=%+v identities=%+v", h.q.insertedAccounts, h.q.insertIdentitys)
	}
	if avatars.calls != 1 || avatars.provider.ID != provider.ID {
		t.Fatalf("Steam invite avatar inheritance = %+v", avatars)
	}
}

// seedUnboundInvite seeds a redeemable invite with no provider binding, so
// the invitee must select one via the provider query parameter.
func seedUnboundInvite(t *testing.T, h *fedTestHarness, token string) {
	t.Helper()
	enr := validInvite(token, h.idp.Slug)
	enr.ExpectedUpstreamIdpSlug = pgtype.Text{Valid: false}
	h.q.seedEnrollment(enr)
}

func TestEnrollmentStartFederation_UnboundInviteWithProviderStarts(t *testing.T) {
	h := newInviteTestServer(t)
	seedUnboundInvite(t, h, "tok-pick")

	loc, resp := h.driveStartFederation(t, "tok-pick", "/me", h.idp.Slug)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, "/federation/flow/") && !strings.HasPrefix(loc, h.opTS.URL) {
		t.Fatalf("location = %q, want a federation flow or upstream redirect", loc)
	}
	// Invite redemption shares the federation /callback, so begin must bind
	// the flow to this browser with the same anti-forgery cookie the login
	// flow uses.
	var binding bool
	for _, cookie := range resp.Cookies() {
		binding = binding || cookie.Name == sessstore.FedStateCookieName && cookie.Value != ""
	}
	if !binding {
		t.Fatal("invite begin omitted browser-binding cookie")
	}
}

func TestEnrollmentStartFederation_UnboundInviteWithoutProviderRejected(t *testing.T) {
	h := newInviteTestServer(t)
	seedUnboundInvite(t, h, "tok-noslug-arg")

	_, resp := h.driveStartFederation(t, "tok-noslug-arg", "/me")
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
}

func TestEnrollmentStartFederation_BoundInviteWithDifferingProviderRejected(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-mismatch", h.idp.Slug))

	_, resp := h.driveStartFederation(t, "tok-mismatch", "/me", "some-other-idp")
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
	assertAuditReason(t, h.q, "invite_slug_mismatch")
}

func TestEnrollmentStartFederation_BoundInviteWithoutProviderStillStarts(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-bound", h.idp.Slug))

	loc, resp := h.driveStartFederation(t, "tok-bound", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, "/federation/flow/") && !strings.HasPrefix(loc, h.opTS.URL) {
		t.Fatalf("location = %q, want a federation flow or upstream redirect", loc)
	}
}

func TestEnrollmentStartFederation_LinkOnlyProviderRejected(t *testing.T) {
	h := newInviteTestServer(t)
	linkOnly := h.idp
	linkOnly.Mode = fedoidc.ModeLinkOnly
	h.q.idpBySlug["link-only-idp"] = linkOnly
	seedUnboundInvite(t, h, "tok-linkonly")

	_, resp := h.driveStartFederation(t, "tok-linkonly", "/me", "link-only-idp")
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/error?error=invite_required&ref=") {
		t.Errorf("Location: want /error?error=invite_required&ref=…, got %q", loc)
	}
	assertAuditReason(t, h.q, "link_only_provision_denied")
}

// assertAuditReason asserts some federation_oidc fail row carries the given
// reason — how BeginInvite's rejections stay distinguishable to operators.
func assertAuditReason(t *testing.T, q *fakeFedQueries, reason string) {
	t.Helper()
	for _, ev := range q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(ev.Detail, &detail); err != nil {
			t.Fatalf("decode audit detail: %v", err)
		}
		if detail["reason"] == reason {
			return
		}
	}
	t.Errorf("no audit fail row with reason=%s; events=%+v", reason, q.events)
}

// fullInviteFlow drives start-federation → authorize → callback and returns
// the confirmation grant cookie value ("token.anti") set by the callback.
func fullInviteFlowGrantCookie(t *testing.T, h *fedTestHarness, token string) string {
	t.Helper()
	loc, resp := h.driveStartFederation(t, token, "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{"code": {code}, "state": {state}, "iss": {iss}}
	resp = h.hitCallback(t, h.idp.Slug, q)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.FedStateCookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatal("confirmation grant cookie not set")
	return ""
}

func TestInviteProvisionConfirmPostOffersLocalSignin(t *testing.T) {
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-offer", h.idp.Slug))
	cookie := fullInviteFlowGrantCookie(t, h, "tok-offer")

	// Confirming the grant (YES) must stamp confirmed_at and answer with
	// offerLocalSignin=true — the invite+provider account has no local
	// credentials yet.
	req, err := http.NewRequest(http.MethodPost, h.srvTS.URL+"/api/prohibitorum/auth/federation/confirm", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: cookie})
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Redirect         string `json:"redirect"`
		OfferLocalSignin bool   `json:"offerLocalSignin"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}
	if !out.OfferLocalSignin {
		t.Errorf("offerLocalSignin = false, want true for invite provisioning")
	}
}

func TestInviteProvisionDeclineThenReloginReturnsToWelcome(t *testing.T) {
	// After provisioning (invite consumed, identity unconfirmed), hitting the
	// provider again resolves the EXISTING identity: the invitee gets a fresh
	// confirmation grant and lands back on /welcome instead of an error. This
	// is the recovery path for "not me" on the confirm page.
	h := newInviteTestServer(t)
	h.q.seedEnrollment(validInvite("tok-recover", h.idp.Slug))
	fullInviteFlowGrantCookie(t, h, "tok-recover")

	// Second visit through the same provider, this time via the plain public
	// login entrypoint: same (iss, sub), now bound to the unconfirmed identity
	// → resolveExisting → fresh /welcome grant. No invite involved anymore.
	resp0, err := h.client.Get(h.srvTS.URL + "/api/prohibitorum/auth/federation/" + h.idp.Slug + "/login?return_to=%2Fme")
	if err != nil {
		t.Fatal(err)
	}
	if resp0.StatusCode != http.StatusFound || !strings.HasPrefix(resp0.Header.Get("Location"), h.opTS.URL) {
		t.Fatalf("second login status/location = %d %q", resp0.StatusCode, resp0.Header.Get("Location"))
	}
	loc := resp0.Header.Get("Location")
	code, state, iss := driveAuthorize(t, loc)
	q := url.Values{"code": {code}, "state": {state}, "iss": {iss}}
	resp := h.hitCallback(t, h.idp.Slug, q)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/welcome" {
		t.Fatalf("second callback status/location = %d %q, want 302 /welcome",
			resp.StatusCode, resp.Header.Get("Location"))
	}
}
