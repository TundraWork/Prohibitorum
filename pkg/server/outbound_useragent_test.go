package server

import (
	"strings"
	"testing"

	"prohibitorum/pkg/federation"
)

// TestIdentifyOutbound_SetsUserAgent pins the NewServer wiring: the outbound
// client must be identified with the same `Prohibitorum/<version> <public
// origin>` UA the VRChat client sends, or UA-gating upstreams (bgm.tv) answer
// every federation request with 403. Deleting the SetOutboundUserAgent call
// inside identifyOutbound fails this test.
func TestIdentifyOutbound_SetsUserAgent(t *testing.T) {
	previous := federation.CurrentOutboundUserAgent()
	t.Cleanup(func() { federation.SetOutboundUserAgent(previous) })

	vrchatClient, err := identifyOutbound("https://login.example.com")
	if err != nil {
		t.Fatalf("identifyOutbound: %v", err)
	}

	want := vrchatClient.UserAgent()
	got := federation.CurrentOutboundUserAgent()
	if got != want {
		t.Errorf("outbound UA after identifyOutbound = %q, want the VRChat client's %q", got, want)
	}
	if !strings.HasPrefix(got, "Prohibitorum/") || !strings.HasSuffix(got, " https://login.example.com") {
		t.Errorf("outbound UA = %q, want Prohibitorum/<version> <public origin>", got)
	}
}
