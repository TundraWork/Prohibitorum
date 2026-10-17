package server

import (
	"net/http"
	"net/url"
	"testing"

	"prohibitorum/pkg/federation"
)

// A requireLocalCredential invitation verifies first: the callback stores the
// identity snapshot on the invite, consumes nothing, creates no account and
// issues no session — the browser goes back to /enroll/{token}.
func TestInviteRequireLocalCallbackStoresSnapshotWithoutAccount(t *testing.T) {
	h := newInviteTestServer(t)
	slug := seedProviderWithMode(t, h, "mockop-requirelocal", federation.ModeInviteOnly)
	enr := validInvite("tok-requirelocal", slug)
	enr.RequireLocalCredential = true
	h.q.seedEnrollment(enr)
	h.op.SetClaims("requirelocal-sub-001", "requirelocal@example.com", true, "requirelocal", "Require Local")
	h.op.SetPicture("https://mockop.example/requirelocal.png")

	loc, resp := h.driveStartFederation(t, "tok-requirelocal", "/me", slug)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start-federation: status %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)
	callback := h.hitCallback(t, slug, url.Values{"code": {code}, "state": {state}, "iss": {iss}})
	if got := callback.Header.Get("Location"); got != "/enroll/tok-requirelocal" {
		t.Fatalf("callback Location = %q, want /enroll/tok-requirelocal", got)
	}

	stored, err := h.q.GetEnrollmentByToken(t.Context(), "tok-requirelocal")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConsumedAt.Valid {
		t.Error("snapshot consumption: invite consumed at verification")
	}
	if !stored.FederatedUpstreamIdpID.Valid || stored.FederatedUpstreamSub.String != "requirelocal-sub-001" ||
		stored.FederatedDisplayName.String != "Require Local" || len(stored.FederatedUpstreamData) == 0 {
		t.Fatalf("stored snapshot = %+v", stored)
	}
	if !stored.FederatedAvatarUrl.Valid || stored.FederatedAvatarUrl.String != "https://mockop.example/requirelocal.png" {
		t.Fatalf("snapshot avatar URL = %+v", stored.FederatedAvatarUrl)
	}
	if len(h.q.insertedAccounts) != 0 || len(h.q.insertIdentitys) != 0 {
		t.Fatalf("verification created accounts=%d identities=%d, want none", len(h.q.insertedAccounts), len(h.q.insertIdentitys))
	}
}
