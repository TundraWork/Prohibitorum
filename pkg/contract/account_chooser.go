package contract

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

var OperationGetAccountChooser = huma.Operation{
	OperationID: "getAccountChooser",
	Method:      http.MethodGet,
	Path:        "/account-chooser",
	Summary:     "Read an account chooser: the application and the accounts signed in on this browser.",
}

var OperationChooseAccount = huma.Operation{
	OperationID: "chooseAccount",
	Method:      http.MethodPost,
	Path:        "/account-chooser",
	Summary:     "Choose the account a protocol request continues as.",
}

var OperationSwitchConsentAccount = huma.Operation{
	OperationID: "switchConsentAccount",
	Method:      http.MethodPost,
	Path:        "/account-chooser/switch",
	Summary:     "Open the account chooser for the request a consent page is showing.",
}

// AccountChooserApp is the application an account chooser picks an account
// for.
type AccountChooserApp struct {
	Kind string `json:"kind" enum:"oidc,saml"`
	// Key names the application for the "last used" mark the dashboard keeps:
	// "oidc:<client_id>" or "saml:<SP entity ID>".
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	IconURL *string `json:"iconUrl,omitempty"`
}

// AccountChooserAccount is one account signed in on this browser, and whether
// it may use the application.
type AccountChooserAccount struct {
	AccountID   int32   `json:"accountId"`
	U           string  `json:"u"`
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	Allowed     bool    `json:"allowed"`
}

// AccountChooser is the body of GET /account-chooser.
type AccountChooser struct {
	App AccountChooserApp `json:"app"`
	// Accounts are in the browser's fixed sign-in order.
	Accounts  []AccountChooserAccount `json:"accounts"`
	LoginHint string                  `json:"loginHint,omitempty"`
	// OtherAccountURL signs in another account and continues the request as
	// it.
	OtherAccountURL string `json:"otherAccountUrl"`
}

// ChooseAccountInput is the body of POST /account-chooser.
type ChooseAccountInput struct {
	Ticket    string `json:"ticket" minLength:"1"`
	AccountID int32  `json:"accountId"`
}

// SwitchConsentAccountInput is the body of POST /account-chooser/switch.
type SwitchConsentAccountInput struct {
	Kind          string `json:"kind" enum:"oidc,saml"`
	ConsentTicket string `json:"consentTicket" minLength:"1"`
	// ReturnTo is the authorize address the OIDC consent page returns to;
	// SAML consent tickets carry their own.
	ReturnTo string `json:"returnTo,omitempty"`
}

// AccountChooserRedirect tells the dashboard where to navigate next.
type AccountChooserRedirect struct {
	Redirect string `json:"redirect"`
}
