package authn

import (
	"context"
	"encoding/json"
	"errors"

	"prohibitorum/pkg/kv"
)

// LookupConsent reads an OIDC consent ticket without consuming it, whichever
// account it belongs to, so a caller can tell a ticket for another account on
// the browser from one that is gone. Missing and malformed tickets return
// (nil, nil).
func LookupConsent(ctx context.Context, store kv.Store, nonce string) (*ConsentTicket, error) {
	var t ConsentTicket
	if ok, err := lookupTicket(ctx, store, consentKeyPrefix, nonce, &t); !ok {
		return nil, err
	}
	return &t, nil
}

// LookupSAMLConsent is LookupConsent for a SAML consent ticket.
func LookupSAMLConsent(ctx context.Context, store kv.Store, nonce string) (*SAMLConsentTicket, error) {
	var t SAMLConsentTicket
	if ok, err := lookupTicket(ctx, store, samlConsentKeyPrefix, nonce, &t); !ok {
		return nil, err
	}
	return &t, nil
}

func lookupTicket(ctx context.Context, store kv.Store, prefix, nonce string, out any) (bool, error) {
	if nonce == "" {
		return false, nil
	}
	val, err := store.Get(ctx, prefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return json.Unmarshal([]byte(val), out) == nil, nil
}
