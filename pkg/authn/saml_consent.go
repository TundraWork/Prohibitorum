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
// carries the opaque nonce. ReturnTo is the exact inbound SSO URL (signed raw
// query preserved) so the assertion flow resumes verbatim after the ack.
type SAMLConsentTicket struct {
	AccountID   int32    `json:"account_id"`
	SPID        int64    `json:"sp_id"`
	EntityID    string   `json:"entity_id"`
	DisplayName string   `json:"display_name"`
	Attributes  []string `json:"attributes"`
	ReturnTo    string   `json:"return_to"`
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
