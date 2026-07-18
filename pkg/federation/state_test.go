package federation

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validFlowState() FlowState {
	return FlowState{
		ProviderID: 7, ProviderSlug: "corp", Protocol: "oidc", Intent: IntentLogin,
		ReturnTo: "/", BrowserDigest: BrowserDigest("browser"), ExpiresAt: time.Now().Add(time.Minute).UTC(),
		AdapterState: json.RawMessage(`{"nonce":"n"}`),
		CurrentAction: NextAction{Kind: ActionRedirect, URL: "https://issuer.test/auth", Public: map[string]any{"label": "continue"}},
	}
}

func TestFlowStateRoundTripAndKeys(t *testing.T) {
	s := validFlowState()
	raw, err := s.Encode()
	if err != nil { t.Fatal(err) }
	got, err := DecodeFlowState(raw)
	if err != nil { t.Fatal(err) }
	if got.ProviderID != s.ProviderID || got.ProviderSlug != s.ProviderSlug || got.Protocol != s.Protocol || got.CurrentAction.URL != s.CurrentAction.URL {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !strings.HasPrefix(FlowKey("abc"), "federation:flow:") { t.Fatalf("FlowKey = %q", FlowKey("abc")) }
	if !strings.HasPrefix(FlowLockKey("abc"), "federation:flow:") { t.Fatalf("FlowLockKey = %q", FlowLockKey("abc")) }
}

func TestIntentEnrollFlowStateRoundTripRejectsUnrelatedBindings(t *testing.T) {
	s := validFlowState()
	s.Protocol = "vrchat"
	s.Intent = IntentEnroll
	s.CurrentAction = NextAction{Kind: ActionCollectIdentity}

	raw, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFlowState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Intent != IntentEnroll {
		t.Fatalf("Intent = %q, want %q", got.Intent, IntentEnroll)
	}

	accountID := int32(9)
	tests := []struct {
		name   string
		mutate func(*FlowState)
	}{
		{"link account", func(state *FlowState) { state.LinkAccountID = &accountID }},
		{"link session", func(state *FlowState) { state.LinkSessionID = "session-1" }},
		{"invitation token", func(state *FlowState) { state.EnrollmentToken = "invite-token" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := s
			test.mutate(&invalid)
			if _, err := invalid.Encode(); err == nil {
				t.Fatal("IntentEnroll accepted an unrelated binding")
			}
		})
	}
}

func TestDecodeFlowStateRejectsInvalidBindings(t *testing.T) {
	tests := []struct{name string; mutate func(*FlowState)}{
		{"unknown intent", func(s *FlowState) { s.Intent = "bogus" }},
		{"unknown action", func(s *FlowState) { s.CurrentAction.Kind = "bogus" }},
		{"missing provider id", func(s *FlowState) { s.ProviderID = 0 }},
		{"missing provider slug", func(s *FlowState) { s.ProviderSlug = "" }},
		{"missing protocol", func(s *FlowState) { s.Protocol = "" }},
		{"missing browser digest", func(s *FlowState) { s.BrowserDigest = "" }},
		{"missing expiry", func(s *FlowState) { s.ExpiresAt = time.Time{} }},
		{"oversized adapter state", func(s *FlowState) { s.AdapterState = json.RawMessage(`"` + strings.Repeat("x", maxAdapterStateBytes) + `"`) }},
		{"oversized public state", func(s *FlowState) { s.CurrentAction.Public = map[string]any{"x": strings.Repeat("x", maxPublicStateBytes)} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validFlowState(); tt.mutate(&s)
			raw, err := json.Marshal(s); if err != nil { t.Fatal(err) }
			if _, err := DecodeFlowState(string(raw)); err == nil { t.Fatal("invalid state accepted") }
		})
	}
}

func TestBrowserBindingRequiredAndConstantTimeDigest(t *testing.T) {
	digest := BrowserDigest("browser-token")
	if !BrowserBindingOK(digest, "browser-token") { t.Fatal("matching browser token rejected") }
	if BrowserBindingOK(digest, "") { t.Fatal("empty browser token accepted") }
	if BrowserBindingOK("", "browser-token") { t.Fatal("empty persisted digest accepted") }
	if BrowserBindingOK(digest, "other") { t.Fatal("wrong browser token accepted") }

	sum := sha256.Sum256([]byte("browser-token"))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if digest != want { t.Fatalf("BrowserDigest = %q, want %q", digest, want) }
}

func TestActionValidationRejectsReservedPublicKey(t *testing.T) {
	action := NextAction{Kind: ActionCollectIdentity, Public: map[string]any{"requiresLocalUsername": false}}
	if err := validateAdapterAction(action); err == nil { t.Fatal("adapter reserved key accepted") }
}
