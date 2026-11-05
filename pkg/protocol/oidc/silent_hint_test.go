package oidc

import (
	"context"
	"testing"

	"prohibitorum/pkg/appaccess"
)

// silentAnswer runs prompt=none with the given hint parameters and returns
// "code" or the error the relying party receives.
func silentAnswer(t *testing.T, h *multiHarness, hints map[string]string) string {
	t.Helper()
	v := baseParams()
	v.Set("prompt", "none")
	for k, val := range hints {
		v.Set(k, val)
	}
	_, q := locationQuery(t, h.authorize(t, v))
	if q.Get("code") != "" {
		return "code"
	}
	return q.Get("error")
}

// A relying party probing with prompt=none and a hint learns nothing about
// which accounts are signed in here beyond the ones it could be given anyway:
// a hinted account that is absent, may not use the client, or has not
// consented to it all answer login_required.
func TestAuthorizeSilentHintRevealsNoOtherAccount(t *testing.T) {
	h := newMultiHarness(t)
	h.q.client.RequireConsent = true
	h.signIn(t, alice)
	h.signIn(t, carol) // signed in here, never used rp-1
	h.signIn(t, bob)   // signed in here, may use rp-1, never consented
	h.access.evaluate = func(_ context.Context, id int32, _ string) (appaccess.Decision, error) {
		return appaccess.Decision{Allowed: id != carol}, nil
	}
	h.q.granted[alice] = []string{"openid", "profile"}

	for _, hint := range []string{"carol", "bob", "dave", "nobody"} {
		if got := silentAnswer(t, h, map[string]string{"login_hint": hint}); got != "login_required" {
			t.Errorf("prompt=none login_hint=%s → %s, want login_required (indistinguishable from not signed in)", hint, got)
		}
	}
	for _, id := range []int32{carol, bob, dave} {
		if got := silentAnswer(t, h, map[string]string{"id_token_hint": h.hint(t, id, h.sids[id])}); got != "login_required" {
			t.Errorf("prompt=none id_token_hint for %d → %s, want login_required", id, got)
		}
	}
	// The account the client may use and was consented to still renews.
	if got := silentAnswer(t, h, map[string]string{"login_hint": "alice"}); got != "code" {
		t.Errorf("prompt=none login_hint=alice → %s, want a code", got)
	}
	if got := silentAnswer(t, h, map[string]string{"id_token_hint": h.hint(t, alice, h.sids[alice])}); got != "code" {
		t.Errorf("prompt=none id_token_hint for alice → %s, want a code", got)
	}
}

// id_token_hint naming an account that is no longer signed in here never
// switches to the account that is: silent renewal answers login_required,
// an interactive request asks.
func TestAuthorizeHintForAbsentAccountDoesNotSwitch(t *testing.T) {
	h := newMultiHarness(t)
	h.signIn(t, alice)
	hint := h.hint(t, bob, "an-old-bob-session")

	if got := silentAnswer(t, h, map[string]string{"id_token_hint": hint}); got != "login_required" {
		t.Fatalf("prompt=none for absent bob with alice here → %s, want login_required", got)
	}

	v := baseParams()
	v.Set("id_token_hint", hint)
	rec := h.authorize(t, v)
	if ticket := h.chooserTicket(t, rec); ticket == nil || ticket.LoginHint != "bob" {
		t.Fatalf("interactive request for absent bob must open the chooser pre-filled with bob, got %+v", ticket)
	}

	// Without a hint a single account is still used as before.
	if got := silentAnswer(t, h, nil); got != "code" {
		t.Fatalf("prompt=none without a hint → %s, want a code for the only account", got)
	}
}
