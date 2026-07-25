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
