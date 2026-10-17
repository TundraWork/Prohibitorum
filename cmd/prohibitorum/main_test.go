package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	federationoidc "prohibitorum/pkg/federation/providers/oidc"
)

func TestUpstreamCLIDefinition(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"oidc", "steam"} {
		definition, err := upstreamCLIDefinition(protocol)
		if err != nil {
			t.Fatalf("upstreamCLIDefinition(%q): %v", protocol, err)
		}
		if definition.Protocol() != protocol {
			t.Fatalf("upstreamCLIDefinition(%q).Protocol() = %q", protocol, definition.Protocol())
		}
	}
	if _, err := upstreamCLIDefinition("vrchat"); err == nil {
		t.Fatal("upstreamCLIDefinition(vrchat) accepted an unsupported CLI protocol")
	}
}

func TestUpstreamCLIConfigUsesAdapterWireShape(t *testing.T) {
	t.Parallel()

	raw, definition, err := upstreamCLIConfig(
		"oidc",
		"https://issuer.example",
		"client-id",
		nil,
		nil,
		"",
		"",
		"",
		"",
		true,
		false,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Protocol() != "oidc" {
		t.Fatalf("definition.Protocol() = %q, want oidc", definition.Protocol())
	}

	var got federationoidc.Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := federationoidc.Config{
		ConfigurationMode: "discovery", TokenAuthMethod: "discovery", PKCEMethod: "S256",
		IssuerURL:            "https://issuer.example",
		ClientID:             "client-id",
		Scopes:               []string{"openid", "profile", "email"},
		AllowedDomains:       []string{},
		UsernameClaim:        "preferred_username",
		DisplayNameClaim:     "name",
		EmailClaim:           "email",
		PictureClaim:         "picture",
		SubjectClaim:         "sub",
		RequireVerifiedEmail: true,
		AllowPrivateNetwork:  false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config = %#v, want %#v", got, want)
	}
}

func TestUpstreamCLIConfigUsesEmptySteamConfig(t *testing.T) {
	t.Parallel()

	raw, definition, err := upstreamCLIConfig(
		"steam",
		"",
		"",
		nil,
		nil,
		"",
		"",
		"",
		"",
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Protocol() != "steam" {
		t.Fatalf("definition.Protocol() = %q, want steam", definition.Protocol())
	}
	if string(raw) != "{}" {
		t.Fatalf("config = %s, want {}", raw)
	}
}

// TestUpstreamIDPFlags guards the flag registrations themselves: `create` and
// `update` must both keep --allow-private-network while carrying the newer
// --authenticate-avatar-fetch, or an invocation that names the older flag
// dies with `unknown flag` long before any config validation runs.
func TestUpstreamIDPFlags(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "prohibitorum"}
	addUpstreamIDPCommands(root)

	for _, verb := range []string{"create", "update"} {
		cmd := findSubcommand(t, root, "upstream-idp", verb)
		for _, flag := range []string{"allow-private-network", "authenticate-avatar-fetch"} {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("upstream-idp %s is missing --%s", verb, flag)
			}
		}
	}
}

func findSubcommand(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cmd := root
	for _, name := range path {
		next, _, err := cmd.Find([]string{name})
		if err != nil || next == nil || next == cmd {
			t.Fatalf("command %v not found under %q", path, cmd.Name())
		}
		cmd = next
	}
	return cmd
}
