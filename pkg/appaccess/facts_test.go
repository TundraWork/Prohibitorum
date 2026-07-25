package appaccess

import (
	"errors"
	"reflect"
	"testing"

	"prohibitorum/pkg/db"
)

func TestFactsFromRow(t *testing.T) {
	row := db.GetAccountAccessFactsRow{
		ID:                     7,
		Disabled:               false,
		HasPasskey:             true,
		HasPasswordTotp:        true,
		ConfirmedProviderSlugs: []string{"corp", "steam", "corp"},
		ConfirmedProtocols:     []string{"oidc", "steam", "oidc"},
		HasFederation:          true,
		HasAnyAvatar:           true,
		HasUserAvatar:          false,
	}

	facts, err := factsFromRow(row)
	if err != nil {
		t.Fatalf("factsFromRow() error = %v", err)
	}
	if !facts.HasPasskey || !facts.HasPasswordTOTP || facts.HasUserAvatar {
		t.Fatalf("facts = %#v", facts)
	}
	if got, want := facts.ConfirmedProviders, map[string]struct{}{"corp": {}, "steam": {}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfirmedProviders = %#v, want %#v", got, want)
	}
	if got, want := facts.ConfirmedProtocols, map[string]struct{}{"oidc": {}, "steam": {}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConfirmedProtocols = %#v, want %#v", got, want)
	}
}

func TestFactsFromRowRejectsDisabledAccount(t *testing.T) {
	_, err := factsFromRow(db.GetAccountAccessFactsRow{ID: 7, Disabled: true})
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("factsFromRow() error = %v, want ErrAccountDisabled", err)
	}
}
