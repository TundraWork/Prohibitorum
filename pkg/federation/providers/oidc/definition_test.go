package oidc

import (
	"encoding/json"
	"reflect"
	"testing"

	federationcore "prohibitorum/pkg/federation"
)

const validDefinitionConfig = `{
  "issuerUrl":"https://issuer.example",
  "clientId":"client-id",
  "scopes":["openid","profile","email"],
  "allowedDomains":[],
  "usernameClaim":"preferred_username",
  "displayNameClaim":"name",
  "emailClaim":"email",
  "pictureClaim":"picture",
  "requireVerifiedEmail":true,
  "allowPrivateNetwork":false
}`

func TestDefinitionDescriptor(t *testing.T) {
	t.Parallel()

	got := (Definition{}).Descriptor()
	want := federationcore.Descriptor{
		Protocol: Protocol,
		SearchFields: []federationcore.SearchField{
			{Key: "subject", Operators: []federationcore.SearchOperator{federationcore.SearchExact}},
			{Key: "email", Operators: []federationcore.SearchOperator{federationcore.SearchExact, federationcore.SearchPrefix, federationcore.SearchContains}},
		},
		RequiresSecret: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Descriptor() = %#v, want %#v", got, want)
	}
}

func TestDefinitionValidateConfig(t *testing.T) {
	t.Parallel()

	definition := Definition{}
	for _, tc := range []struct {
		name    string
		config  json.RawMessage
		wantErr bool
	}{
		{name: "exact config", config: json.RawMessage(validDefinitionConfig)},
		{name: "missing config", config: nil, wantErr: true},
		{name: "non-object", config: json.RawMessage(`[]`), wantErr: true},
		{name: "unknown field", config: json.RawMessage(validDefinitionConfig[:len(validDefinitionConfig)-2] + `,"extra":true}`), wantErr: true},
		{name: "missing allowed domains", config: json.RawMessage(`{"issuerUrl":"https://issuer.example","clientId":"client-id","scopes":["openid"],"usernameClaim":"preferred_username","displayNameClaim":"name","emailClaim":"email","pictureClaim":"picture","requireVerifiedEmail":true,"allowPrivateNetwork":false}`), wantErr: true},
		{name: "empty client id", config: json.RawMessage(`{"issuerUrl":"https://issuer.example","clientId":"","scopes":["openid"],"allowedDomains":[],"usernameClaim":"preferred_username","displayNameClaim":"name","emailClaim":"email","pictureClaim":"picture","requireVerifiedEmail":true,"allowPrivateNetwork":false}`), wantErr: true},
		{name: "unsafe issuer", config: json.RawMessage(`{"issuerUrl":"http://127.0.0.1","clientId":"client-id","scopes":["openid"],"allowedDomains":[],"usernameClaim":"preferred_username","displayNameClaim":"name","emailClaim":"email","pictureClaim":"picture","requireVerifiedEmail":true,"allowPrivateNetwork":false}`), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := definition.ValidateConfig(tc.config)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateConfig(%s) error = %v, wantErr %v", tc.config, err, tc.wantErr)
			}
		})
	}
}

func TestDefinitionReadyIgnoresDisabledState(t *testing.T) {
	t.Parallel()

	definition := Definition{}
	provider := federationcore.Provider{
		Protocol:     Protocol,
		Config:       json.RawMessage(validDefinitionConfig),
		Secret:       &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1},
		SecretStatus: "valid",
		Disabled:     true,
	}
	if !definition.Ready(provider) {
		t.Fatal("ready provider must remain ready while disabled")
	}
	provider.SecretStatus = "configured"
	if !definition.Ready(provider) {
		t.Fatal("migrated configured provider must remain ready")
	}
	provider.SecretStatus = "invalid"
	if definition.Ready(provider) {
		t.Fatal("invalid secret status must not be ready")
	}
}
