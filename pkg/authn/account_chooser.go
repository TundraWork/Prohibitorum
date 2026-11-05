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

// AccountChooserTTL bounds how long an account choice stays usable. Within it
// the ticket can be used repeatedly: re-authentication and the consent page
// both come back to the same protocol URL carrying account_choice.
const AccountChooserTTL = 10 * time.Minute

const accountChooserKeyPrefix = "account-chooser:"

// Account chooser ticket kinds: the flow the choice resumes.
const (
	ChooserKindOIDC            = "oidc"
	ChooserKindSAML            = "saml"
	ChooserKindSAMLInit        = "saml_init"
	ChooserKindRequireIdentity = "require_identity"
)

// AccountChooserTicket records an account choice for one protocol request.
// Protocol endpoints are full-page navigations that cannot carry the
// X-Prohibitorum-Account header, so the chooser page writes the choice here
// and the endpoint reads it back from account_choice=<nonce>. A relying party
// cannot forge one, and Kind/App bind it to the request it was made for.
type AccountChooserTicket struct {
	Kind string `json:"kind"`
	// App is the OIDC client_id or the SAML SP entity ID.
	App string `json:"app"`
	// ResumeURL is the same-origin protocol URL the choice resumes, with its
	// original query.
	ResumeURL string `json:"resume_url"`
	LoginHint string `json:"login_hint,omitempty"`
	// AccountID is 0 until an account is chosen; it can be chosen again.
	AccountID int32     `json:"account_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateAccountChooser stores t under a fresh 32-byte nonce.
func CreateAccountChooser(ctx context.Context, store kv.Store, t AccountChooserTicket) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	t.ExpiresAt = time.Now().Add(AccountChooserTTL)
	if err := putAccountChooser(ctx, store, nonce, &t); err != nil {
		return "", err
	}
	return nonce, nil
}

// GetAccountChooser reads a ticket. Missing, malformed and expired tickets
// return (nil, nil).
func GetAccountChooser(ctx context.Context, store kv.Store, nonce string) (*AccountChooserTicket, error) {
	if nonce == "" {
		return nil, nil
	}
	val, err := store.Get(ctx, accountChooserKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t AccountChooserTicket
	if err := json.Unmarshal([]byte(val), &t); err != nil || time.Now().After(t.ExpiresAt) {
		return nil, nil
	}
	return &t, nil
}

// BindAccountChooser records accountID as the choice, keeping the ticket's
// expiry. It returns the updated ticket, or nil when the ticket is gone.
func BindAccountChooser(ctx context.Context, store kv.Store, nonce string, accountID int32) (*AccountChooserTicket, error) {
	t, err := GetAccountChooser(ctx, store, nonce)
	if err != nil || t == nil {
		return nil, err
	}
	t.AccountID = accountID
	if err := putAccountChooser(ctx, store, nonce, t); err != nil {
		return nil, err
	}
	return t, nil
}

// ChosenAccount returns the account a ticket chose for (kind, app), or 0 when
// the nonce is missing, expired, bound to another flow or not chosen yet.
// Callers still check the account is signed in on this browser.
func ChosenAccount(ctx context.Context, store kv.Store, nonce, kind, app string) int32 {
	t, err := GetAccountChooser(ctx, store, nonce)
	if err != nil || t == nil || t.Kind != kind || t.App != app {
		return 0
	}
	return t.AccountID
}

func putAccountChooser(ctx context.Context, store kv.Store, nonce string, t *AccountChooserTicket) error {
	remaining := time.Until(t.ExpiresAt)
	if remaining <= 0 {
		return nil
	}
	payload, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return store.SetEx(ctx, accountChooserKeyPrefix+nonce, string(payload), remaining)
}
