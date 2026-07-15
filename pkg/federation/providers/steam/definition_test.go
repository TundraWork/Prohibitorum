package steam

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
			{Key: "steamId", Operators: []federationcore.SearchOperator{federationcore.SearchExact}},
			{Key: "personaName", Operators: []federationcore.SearchOperator{federationcore.SearchExact, federationcore.SearchPrefix, federationcore.SearchContains}},
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
		{name: "empty object", config: json.RawMessage(`{}`)},
		{name: "missing", config: nil, wantErr: true},
		{name: "null", config: json.RawMessage(`null`), wantErr: true},
		{name: "array", config: json.RawMessage(`[]`), wantErr: true},
		{name: "non-empty object", config: json.RawMessage(`{"allowPrivateNetwork":true}`), wantErr: true},
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
		Config:       json.RawMessage(`{}`),
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
	provider.SecretStatus = "valid"
	provider.Secret = nil
	if definition.Ready(provider) {
		t.Fatal("provider without secret must not be ready")
	}
}
