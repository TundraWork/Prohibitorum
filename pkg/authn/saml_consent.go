package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"

	"prohibitorum/pkg/kv"
)

const samlConsentKeyPrefix = "saml:consent:"

// SAMLConsentTicket is the server-minted record of a pending SAML advisory
// acknowledgement. Stored in KV under a single-use nonce; the browser only ever
// carries the opaque nonce.
type SAMLConsentTicket struct {
	AccountID   int32    `json:"account_id"`
	SPID        int64    `json:"sp_id"`
	EntityID    string   `json:"entity_id"`
	DisplayName string   `json:"display_name"`
	Attributes  []string `json:"attributes"`
	// Issue context (validated at gate time) so the resume path can emit the
	// assertion without the browser re-sending the original AuthnRequest — this
	// is what makes POST-binding SP-initiated consent work.
	ACSURL       string `json:"acs_url"`
	InResponseTo string `json:"in_response_to"`
	RelayState   string `json:"relay_state"`
	// ForceAuthn carries the request's demand for a fresh sign-in, so an
	// account switched to on the consent page re-authenticates too.
	ForceAuthn bool `json:"force_authn,omitempty"`
}

// DemandSAMLConsent mints a single-use nonce and stores the ticket (reuses the
// OIDC ConsentTicketTTL).
func DemandSAMLConsent(ctx context.Context, store kv.Store, ticket SAMLConsentTicket) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	payload, err := json.Marshal(ticket)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, samlConsentKeyPrefix+nonce, string(payload), ConsentTicketTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// PeekSAMLConsent reads (without consuming) and returns the ticket iff it
// belongs to accountID.
func PeekSAMLConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*SAMLConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Get(ctx, samlConsentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeSAMLConsent(val, accountID)
}

// ReadSAMLConsent reads (without consuming) a ticket whichever account it
// belongs to; nil when missing or malformed. The resume endpoint uses it to
// learn the ticket's account before choosing that account's session.
func ReadSAMLConsent(ctx context.Context, store kv.Store, nonce string) (*SAMLConsentTicket, error) {
	if nonce == "" {
		return nil, nil
	}
	val, err := store.Get(ctx, samlConsentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t SAMLConsentTicket
	if json.Unmarshal([]byte(val), &t) != nil {
		return nil, nil
	}
	return &t, nil
}

// ConsumeSAMLConsent atomically pops the ticket (single use) iff it belongs to
// accountID.
func ConsumeSAMLConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*SAMLConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Pop(ctx, samlConsentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeSAMLConsent(val, accountID)
}

func decodeSAMLConsent(val string, accountID int32) (*SAMLConsentTicket, bool, error) {
	var t SAMLConsentTicket
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return nil, false, nil // malformed → treat as absent
	}
	if t.AccountID != accountID {
		return nil, false, nil // bound to a different account
	}
	return &t, true, nil
}
