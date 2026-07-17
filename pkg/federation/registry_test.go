package federation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeDefinition struct {
	protocol   string
	descriptor Descriptor
}

func (d fakeDefinition) Protocol() string                       { return d.protocol }
func (d fakeDefinition) Descriptor() Descriptor                { return d.descriptor }
func (d fakeDefinition) ValidateConfig(json.RawMessage) error  { return nil }
func (d fakeDefinition) ValidateSecret([]byte) error            { return nil }
func (d fakeDefinition) Ready(Provider) bool                    { return true }

type fakeAdapter struct{ protocol string }

func (a fakeAdapter) Protocol() string { return a.protocol }
func (a fakeAdapter) Begin(context.Context, Provider, BeginContext) (json.RawMessage, NextAction, error) {
	return json.RawMessage(`{"step":1}`), NextAction{Kind: ActionRedirect, URL: "https://example.test"}, nil
}
func (a fakeAdapter) Advance(context.Context, Provider, json.RawMessage, ActionInput) (AdvanceResult, error) {
	return AdvanceResult{}, nil
}

func descriptor(protocol string) Descriptor {
	return Descriptor{Protocol: protocol, SearchFields: []SearchField{{Key: "subject", Operators: []SearchOperator{SearchExact}}}}
}

func TestRegistryDefinitionMayPrecedeAdapter(t *testing.T) {
	r := NewRegistry()
	d := fakeDefinition{protocol: "fake", descriptor: descriptor("fake")}
	if err := r.RegisterDefinition(d); err != nil {
		t.Fatalf("RegisterDefinition: %v", err)
	}
	if got, err := r.Definition("fake"); err != nil || got.Protocol() != "fake" {
		t.Fatalf("Definition = %v, %v", got, err)
	}
	if _, err := r.Adapter("fake"); err == nil {
		t.Fatal("Adapter succeeded before registration")
	}
	if err := r.RegisterAdapter(fakeAdapter{protocol: "fake"}); err != nil {
		t.Fatalf("RegisterAdapter: %v", err)
	}
	if got, err := r.Adapter("fake"); err != nil || got.Protocol() != "fake" {
		t.Fatalf("Adapter = %v, %v", got, err)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Registry) error
	}{
		{"invalid protocol", func(r *Registry) error { return r.RegisterDefinition(fakeDefinition{protocol: "OIDC", descriptor: descriptor("OIDC")}) }},
		{"empty descriptor protocol", func(r *Registry) error { return r.RegisterDefinition(fakeDefinition{protocol: "fake"}) }},
		{"descriptor mismatch", func(r *Registry) error { return r.RegisterDefinition(fakeDefinition{protocol: "fake", descriptor: descriptor("other")}) }},
		{"duplicate field", func(r *Registry) error { d := descriptor("fake"); d.SearchFields = append(d.SearchFields, d.SearchFields[0]); return r.RegisterDefinition(fakeDefinition{protocol: "fake", descriptor: d}) }},
		{"empty operators", func(r *Registry) error { d := descriptor("fake"); d.SearchFields[0].Operators = nil; return r.RegisterDefinition(fakeDefinition{protocol: "fake", descriptor: d}) }},
		{"duplicate operator", func(r *Registry) error { d := descriptor("fake"); d.SearchFields[0].Operators = []SearchOperator{SearchExact, SearchExact}; return r.RegisterDefinition(fakeDefinition{protocol: "fake", descriptor: d}) }},
		{"unknown operator", func(r *Registry) error { d := descriptor("fake"); d.SearchFields[0].Operators = []SearchOperator{"regex"}; return r.RegisterDefinition(fakeDefinition{protocol: "fake", descriptor: d}) }},
		{"adapter missing definition", func(r *Registry) error { return r.RegisterAdapter(fakeAdapter{protocol: "fake"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(NewRegistry()); err == nil {
				t.Fatal("registration unexpectedly succeeded")
			}
		})
	}
}

func TestRegistryRejectsDuplicatesAndProtocolMismatch(t *testing.T) {
	r := NewRegistry()
	d := fakeDefinition{protocol: "fake", descriptor: descriptor("fake")}
	if err := r.RegisterDefinition(d); err != nil { t.Fatal(err) }
	if err := r.RegisterDefinition(d); err == nil { t.Fatal("duplicate definition succeeded") }
	if err := r.RegisterAdapter(fakeAdapter{protocol: "other"}); err == nil { t.Fatal("mismatched adapter succeeded") }
	if err := r.RegisterAdapter(fakeAdapter{protocol: "fake"}); err != nil { t.Fatal(err) }
	if err := r.RegisterAdapter(fakeAdapter{protocol: "fake"}); err == nil { t.Fatal("duplicate adapter succeeded") }
}

func TestRegistryLookupsFailClosedAndDescriptorsAreCopied(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Definition("missing"); !errors.Is(err, ErrUnknownProtocol) { t.Fatalf("Definition error = %v", err) }
	if _, err := r.Adapter("missing"); !errors.Is(err, ErrUnknownProtocol) { t.Fatalf("Adapter error = %v", err) }
	if _, err := r.Descriptor("missing"); !errors.Is(err, ErrUnknownProtocol) { t.Fatalf("Descriptor error = %v", err) }

	d := fakeDefinition{protocol: "fake", descriptor: descriptor("fake")}
	if err := r.RegisterDefinition(d); err != nil { t.Fatal(err) }
	got, err := r.Descriptor("fake")
	if err != nil { t.Fatal(err) }
	got.SearchFields[0].Key = "mutated"
	got.SearchFields[0].Operators[0] = SearchContains
	again, err := r.Descriptor("fake")
	if err != nil { t.Fatal(err) }
	if again.SearchFields[0].Key != "subject" || again.SearchFields[0].Operators[0] != SearchExact {
		t.Fatalf("descriptor mutation leaked: %+v", again)
	}
}
