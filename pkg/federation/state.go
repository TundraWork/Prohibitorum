package federation

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxAdapterStateBytes = 4 << 10
	maxPublicStateBytes  = 4 << 10
)

type FlowState struct {
	ProviderID      int64           `json:"provider_id"`
	ProviderSlug    string          `json:"provider_slug"`
	Protocol        string          `json:"protocol"`
	Intent          Intent          `json:"intent"`
	ReturnTo        string          `json:"return_to"`
	BrowserDigest   string          `json:"browser_digest"`
	LinkAccountID   *int32          `json:"link_account_id,omitempty"`
	LinkSessionID   string          `json:"link_session_id,omitempty"`
	EnrollmentToken string          `json:"enrollment_token,omitempty"`
	ExpiresAt       time.Time       `json:"expires_at"`
	AdapterState    json.RawMessage `json:"adapter_state"`
	CurrentAction   NextAction      `json:"current_action"`
}

func (s FlowState) Encode() (string, error) {
	if err := validateFlowState(s); err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("federation: encode flow state: %w", err)
	}
	return string(raw), nil
}

func DecodeFlowState(raw string) (*FlowState, error) {
	var state FlowState
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("federation: decode flow state: %w", err)
	}
	if err := validateFlowState(state); err != nil {
		return nil, err
	}
	return &state, nil
}

func validateFlowState(state FlowState) error {
	if state.ProviderID <= 0 || state.ProviderSlug == "" || !protocolNameRE.MatchString(state.Protocol) {
		return errors.New("federation: missing provider binding")
	}
	switch state.Intent {
	case IntentLogin, IntentLink, IntentInvite:
	default:
		return errors.New("federation: invalid flow intent")
	}
	if state.BrowserDigest == "" {
		return errors.New("federation: missing browser binding")
	}
	if state.ExpiresAt.IsZero() {
		return errors.New("federation: missing flow expiry")
	}
	if len(state.AdapterState) > maxAdapterStateBytes {
		return errors.New("federation: adapter state too large")
	}
	if len(state.AdapterState) == 0 || !json.Valid(state.AdapterState) {
		return errors.New("federation: invalid adapter state")
	}
	if err := validateStoredAction(state.CurrentAction); err != nil {
		return err
	}
	if state.Intent == IntentLink {
		if state.LinkAccountID == nil || *state.LinkAccountID <= 0 || state.LinkSessionID == "" {
			return errors.New("federation: missing link binding")
		}
	} else if state.LinkAccountID != nil || state.LinkSessionID != "" {
		return errors.New("federation: unexpected link binding")
	}
	if state.Intent == IntentInvite {
		if state.EnrollmentToken == "" {
			return errors.New("federation: missing enrollment binding")
		}
	} else if state.EnrollmentToken != "" {
		return errors.New("federation: unexpected enrollment binding")
	}
	return nil
}

func validateStoredAction(action NextAction) error {
	switch action.Kind {
	case ActionRedirect:
		if action.URL == "" {
			return errors.New("federation: redirect action missing URL")
		}
	case ActionCollectIdentity, ActionPublishProof:
		if action.URL != "" {
			return errors.New("federation: local action cannot contain URL")
		}
	default:
		return errors.New("federation: invalid action kind")
	}
	raw, err := json.Marshal(action.Public)
	if err != nil {
		return fmt.Errorf("federation: encode public action: %w", err)
	}
	if len(raw) > maxPublicStateBytes {
		return errors.New("federation: public action too large")
	}
	return nil
}

func validateAdapterAction(action NextAction) error {
	if action.Public != nil {
		if _, reserved := action.Public["requiresLocalUsername"]; reserved {
			return errors.New("federation: adapter used reserved public key")
		}
	}
	return validateStoredAction(action)
}

func validateAdapterState(state json.RawMessage) error {
	if len(state) == 0 || !json.Valid(state) {
		return errors.New("federation: invalid adapter state")
	}
	if len(state) > maxAdapterStateBytes {
		return errors.New("federation: adapter state too large")
	}
	return nil
}

func FlowKey(token string) string { return "federation:flow:" + token }
func FlowLockKey(token string) string { return "federation:flow:" + token + ":lock" }
func ConfirmKey(token string) string { return "federation:confirm:" + token }

func AvatarFetchKey(accountID int32, providerID int64) string {
	return "federation:avatar:" + strconv.Itoa(int(accountID)) + ":" + strconv.FormatInt(providerID, 10)
}

func AvatarFetchPattern(accountID int32) string {
	return "federation:avatar:" + strconv.Itoa(int(accountID)) + ":*"
}

func BrowserDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func BrowserBindingOK(expectedDigest, browserToken string) bool {
	if expectedDigest == "" || browserToken == "" {
		return false
	}
	actual := BrowserDigest(browserToken)
	return subtle.ConstantTimeCompare([]byte(expectedDigest), []byte(actual)) == 1
}

type ConfirmGrant struct {
	AccountID      int32    `json:"account_id"`
	IdentityID     int64    `json:"identity_id"`
	ProviderID     int64    `json:"provider_id"`
	ProviderSlug   string   `json:"provider_slug"`
	ReturnTo       string   `json:"return_to"`
	BrowserDigest  string   `json:"browser_digest"`
	AMR            []string `json:"amr,omitempty"`
}

func (g ConfirmGrant) Encode() (string, error) {
	if g.AccountID <= 0 || g.IdentityID <= 0 || g.ProviderID <= 0 || g.ProviderSlug == "" || g.BrowserDigest == "" {
		return "", errors.New("federation: invalid confirmation grant")
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return "", fmt.Errorf("federation: encode confirmation grant: %w", err)
	}
	return string(raw), nil
}

func DecodeConfirmGrant(raw string) (*ConfirmGrant, error) {
	var grant ConfirmGrant
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		return nil, fmt.Errorf("federation: decode confirmation grant: %w", err)
	}
	if _, err := grant.Encode(); err != nil {
		return nil, err
	}
	return &grant, nil
}

