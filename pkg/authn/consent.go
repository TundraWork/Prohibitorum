package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"prohibitorum/pkg/kv"
)

// ConsentTicketTTL bounds how long a pending consent decision stays valid.
const ConsentTicketTTL = 10 * time.Minute

const consentKeyPrefix = "oidc:consent:"

// ConsentTicket is the server-minted record of a pending OIDC consent decision.
// It is stored in KV under a single-use nonce and carries everything the
// decision needs so the browser SPA never reconstructs flow state. RedirectURI
// + State let a deny produce a correct access_denied RP redirect.
type ConsentTicket struct {
	AccountID   int32    `json:"account_id"`
	ClientID    string   `json:"client_id"`
	Scopes      []string `json:"scopes"`
	RedirectURI string   `json:"redirect_uri"`
	State       string   `json:"state"`
}

// DemandConsent mints a single-use nonce and stores the ticket (10 min TTL).
func DemandConsent(ctx context.Context, store kv.Store, ticket ConsentTicket) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	payload, err := json.Marshal(ticket)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, consentKeyPrefix+nonce, string(payload), ConsentTicketTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// PeekConsent reads (without consuming) the ticket and returns it iff it belongs
// to accountID. Returns (nil,false,nil) for empty/missing/malformed/wrong-account.
func PeekConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*ConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Get(ctx, consentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeConsent(val, accountID)
}

// ConsumeConsent atomically pops the ticket (single-use) and returns it iff it
// belongs to accountID.
func ConsumeConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*ConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Pop(ctx, consentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeConsent(val, accountID)
}

func decodeConsent(val string, accountID int32) (*ConsentTicket, bool, error) {
	var t ConsentTicket
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return nil, false, nil // malformed → treat as absent
	}
	if t.AccountID != accountID {
		return nil, false, nil // bound to a different account
	}
	return &t, true, nil
}
