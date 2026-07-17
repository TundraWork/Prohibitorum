package vrchat

import (
	"encoding/json"
	"reflect"
	"testing"

	federationcore "prohibitorum/pkg/federation"
)

func TestDefinitionDescriptor(t *testing.T) {
	t.Parallel()

	got := (Definition{}).Descriptor()
	want := federationcore.Descriptor{
		Protocol: Protocol,
		SearchFields: []federationcore.SearchField{
			{Key: "userId", Operators: []federationcore.SearchOperator{federationcore.SearchExact}},
			{Key: "displayName", Operators: []federationcore.SearchOperator{federationcore.SearchExact, federationcore.SearchPrefix, federationcore.SearchContains}},
		},
		SupportsOperator: true,
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
		{name: "empty object", config: json.RawMessage(`{}`)},
		{name: "whitespace object", config: json.RawMessage(" {\n} ")},
		{name: "missing", config: nil, wantErr: true},
		{name: "null", config: json.RawMessage(`null`), wantErr: true},
		{name: "array", config: json.RawMessage(`[]`), wantErr: true},
		{name: "non-empty object", config: json.RawMessage(`{"apiKey":"not-allowed"}`), wantErr: true},
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

func TestDefinitionRejectsGenericSecret(t *testing.T) {
	t.Parallel()

	definition := Definition{}
	if err := definition.ValidateSecret(nil); err != nil {
		t.Fatalf("ValidateSecret(nil) = %v, want nil", err)
	}
	if err := definition.ValidateSecret([]byte("generic-secret")); err == nil {
		t.Fatal("ValidateSecret(non-empty) = nil, want rejection")
	}
}

func TestDefinitionReadyIgnoresDisabledState(t *testing.T) {
	t.Parallel()

	definition := Definition{}
	provider := federationcore.Provider{
		Protocol:     Protocol,
		Config:       json.RawMessage(`{}`),
		Secret:       &federationcore.SealedSecret{Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1},
		SecretStatus: "valid",
		Disabled:     true,
	}
	if !definition.Ready(provider) {
		t.Fatal("ready provider must remain ready while disabled")
	}

	for _, mutate := range []func(*federationcore.Provider){
		func(p *federationcore.Provider) { p.Protocol = "oidc" },
		func(p *federationcore.Provider) { p.Config = json.RawMessage(`{"unexpected":true}`) },
		func(p *federationcore.Provider) { p.Secret = nil },
		func(p *federationcore.Provider) { p.SecretStatus = "configured" },
	} {
		candidate := provider
		mutate(&candidate)
		if definition.Ready(candidate) {
			t.Fatalf("Ready(%#v) = true, want false", candidate)
		}
	}
}

var _ federationcore.Definition = Definition{}
