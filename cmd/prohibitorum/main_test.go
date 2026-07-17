package main

import (
	"encoding/json"
	"reflect"
	"testing"

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
		IssuerURL:            "https://issuer.example",
		ClientID:             "client-id",
		Scopes:               []string{"openid", "profile", "email"},
		AllowedDomains:       []string{},
		UsernameClaim:        "preferred_username",
		DisplayNameClaim:     "name",
		EmailClaim:           "email",
		PictureClaim:         "picture",
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
