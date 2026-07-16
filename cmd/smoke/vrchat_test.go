package main

import (
	"encoding/json"
	"testing"
)

func TestOIDCSeedProviderConfigUsesCurrentPluginSchema(t *testing.T) {
	raw, err := oidcSeedProviderConfig("https://issuer.example", "client", []string{"example.com"}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config) != 10 || config["issuerUrl"] != "https://issuer.example" || config["clientId"] != "client" || config["pictureClaim"] != "picture" || config["requireVerifiedEmail"] != true || config["allowPrivateNetwork"] != true {
		t.Fatalf("config = %#v", config)
	}
}
