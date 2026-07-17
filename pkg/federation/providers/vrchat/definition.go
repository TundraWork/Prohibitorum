package vrchat

import (
	"encoding/json"
	"errors"

	federationcore "prohibitorum/pkg/federation"
)

const Protocol = "vrchat"

type Definition struct{}

func (Definition) Protocol() string { return Protocol }

func (Definition) Descriptor() federationcore.Descriptor {
	return federationcore.Descriptor{
		Protocol: Protocol,
		SearchFields: []federationcore.SearchField{
			{Key: "userId", Operators: []federationcore.SearchOperator{federationcore.SearchExact}},
			{Key: "displayName", Operators: []federationcore.SearchOperator{federationcore.SearchExact, federationcore.SearchPrefix, federationcore.SearchContains}},
		},
		SupportsOperator: true,
	}
}

func (Definition) ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("federation/vrchat: missing config")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return errors.New("federation/vrchat: config must be an empty object")
	}
	if config == nil || len(config) != 0 {
		return errors.New("federation/vrchat: config must be an empty object")
	}
	return nil
}

func (Definition) ValidateSecret(secret []byte) error {
	if len(secret) != 0 {
		return errors.New("federation/vrchat: generic secret input is not supported")
	}
	return nil
}

func (definition Definition) Ready(provider federationcore.Provider) bool {
	return provider.Protocol == Protocol &&
		provider.Secret != nil &&
		len(provider.Secret.Ciphertext) != 0 &&
		len(provider.Secret.Nonce) != 0 &&
		provider.Secret.KeyVersion > 0 &&
		provider.SecretStatus == "valid" &&
		definition.ValidateConfig(provider.Config) == nil
}

var _ federationcore.Definition = Definition{}
