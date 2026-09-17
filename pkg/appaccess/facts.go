package appaccess

import (
	"errors"
	"fmt"

	"prohibitorum/pkg/db"
)

// ErrAccountDisabled prevents a disabled account from receiving an access decision.
var ErrAccountDisabled = errors.New("appaccess: account disabled")

// factsFromRow projects one live access-fact query row into evaluator facts.
func factsFromRow(row db.GetAccountAccessFactsRow) (Facts, error) {
	return factsFromValues(
		row.ID,
		row.Disabled,
		row.HasPasskey,
		row.HasPasswordTotp,
		row.HasFederation,
		row.ConfirmedProviderSlugs,
		row.ConfirmedProtocols,
		row.HasAnyAvatar,
		row.HasUserAvatar,
	)
}

// FactsFromRow converts the generated live account projection for callers
// that must use the exact authorization fact semantics.
func FactsFromRow(row db.GetAccountAccessFactsRow) (Facts, error) { return factsFromRow(row) }

func factsFromPageRow(row db.ListActiveAccountAccessFactsPageRow) (Facts, error) {
	return factsFromValues(
		row.ID,
		row.Disabled,
		row.HasPasskey,
		row.HasPasswordTotp,
		row.HasFederation,
		row.ConfirmedProviderSlugs,
		row.ConfirmedProtocols,
		row.HasAnyAvatar,
		row.HasUserAvatar,
	)
}

// FactsFromPageRow converts a paged active-account projection for previews.
func FactsFromPageRow(row db.ListActiveAccountAccessFactsPageRow) (Facts, error) {
	return factsFromPageRow(row)
}

func factsFromActiveRow(row db.ListActiveAccountAccessFactsRow) (Facts, error) {
	return factsFromValues(
		row.ID,
		row.Disabled,
		row.HasPasskey,
		row.HasPasswordTotp,
		row.HasFederation,
		row.ConfirmedProviderSlugs,
		row.ConfirmedProtocols,
		row.HasAnyAvatar,
		row.HasUserAvatar,
	)
}

// FactsFromActiveRow converts the unpaged active-account projection for previews.
func FactsFromActiveRow(row db.ListActiveAccountAccessFactsRow) (Facts, error) {
	return factsFromActiveRow(row)
}

func factsFromValues(
	accountID int32,
	disabled bool,
	hasPasskey bool,
	hasPasswordTotp bool,
	hasFederation bool,
	confirmedProviderSlugs []string,
	confirmedProtocols []string,
	hasAnyAvatar bool,
	hasUserAvatar bool,
) (Facts, error) {
	if disabled {
		return Facts{}, fmt.Errorf("%w: %d", ErrAccountDisabled, accountID)
	}
	return Facts{
		ConfirmedProviders: stringSet(confirmedProviderSlugs),
		ConfirmedProtocols: stringSet(confirmedProtocols),
		HasPasskey:         hasPasskey,
		HasPasswordTOTP:    hasPasswordTotp,
		HasFederation:      hasFederation,
		HasAnyAvatar:       hasAnyAvatar,
		HasUserAvatar:      hasUserAvatar,
	}, nil
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
