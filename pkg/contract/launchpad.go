package contract

import (
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// LaunchpadApp is one launchable app on the end-user "My apps" home. Kind is
// "oidc" | "forward_auth" | "saml" (drives the tile's type chip). LaunchURL is
// always non-empty (non-launchable apps are omitted server-side). IconURL is nil
// when the app has no uploaded icon.
type LaunchpadApp struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	IconURL   *string `json:"iconUrl,omitempty"`
	LaunchURL string  `json:"launchUrl"`
	// AccentColor is a representative "#rrggbb" extracted from the uploaded icon
	// (server-side, at upload time) for tinting the tile backdrop. Nil when the
	// app has no icon — the client then derives a tint from the name.
	AccentColor *string `json:"accentColor,omitempty"`
}

// ConsentedApp is one app the account has granted OIDC consent to.
type ConsentedApp struct {
	ClientID  string    `json:"clientId"`
	Name      string    `json:"name"`
	IconURL   *string   `json:"iconUrl,omitempty"`
	Scopes    []string  `json:"scopes"`
	GrantedAt time.Time `json:"grantedAt"`
}

// RevokeConsentInput is the body of POST /me/consent/revoke.
type RevokeConsentInput struct {
	ClientID string `json:"clientId"`
}

var OperationListMyApps = huma.Operation{
	OperationID: "listMyApps",
	Method:      http.MethodGet,
	Path:        "/me/apps",
	Summary:     "List the apps the signed-in account may launch",
}

var OperationListMyConsent = huma.Operation{
	OperationID: "listMyConsent",
	Method:      http.MethodGet,
	Path:        "/me/consent",
	Summary:     "List the apps the signed-in account has granted access to",
}

var OperationRevokeConsent = huma.Operation{
	OperationID: "revokeMyConsent",
	Method:      http.MethodPost,
	Path:        "/me/consent/revoke",
	Summary:     "Revoke the signed-in account's consent for an app",
}
