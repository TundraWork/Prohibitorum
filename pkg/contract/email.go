package contract

import (
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// EmailAddressView is one address row. VerifiedVia is one of code, admin,
// upstream; VerifiedProvider is the upstream's display name when the address
// was absorbed from it; VerifiedBy is the asserting admin's username, only
// ever populated on admin-facing responses.
type EmailAddressView struct {
	ID               int64      `json:"id"`
	Address          string     `json:"address"`
	Verified         bool       `json:"verified"`
	VerifiedVia      string     `json:"verifiedVia,omitempty"`
	VerifiedProvider string     `json:"verifiedProvider,omitempty"`
	VerifiedBy       string     `json:"verifiedBy,omitempty"`
	Primary          bool       `json:"primary"`
	CreatedAt        time.Time  `json:"createdAt"`
	VerifiedAt       *time.Time `json:"verifiedAt,omitempty"`
}

// MyEmailsView is the response of GET /me/emails. Emails is sorted primary
// first, then by creation time; DeliveryAvailable reports whether SMTP is
// configured at all; Limit is the per-account address cap.
type MyEmailsView struct {
	Emails            []EmailAddressView `json:"emails"`
	DeliveryAvailable bool               `json:"deliveryAvailable"`
	Limit             int                `json:"limit"`
}

// AccountEmailsView is the admin-facing variant; same ordering, no delivery
// flag (admin assertion never mails).
type AccountEmailsView struct {
	Emails []EmailAddressView `json:"emails"`
	Limit  int                `json:"limit"`
}

var OperationListMyEmails = huma.Operation{
	OperationID: "listMyEmails",
	Method:      http.MethodGet,
	Path:        "/me/emails",
	Summary:     "List the caller's email addresses.",
}

var OperationAddMyEmail = huma.Operation{
	OperationID: "addMyEmail",
	Method:      http.MethodPost,
	Path:        "/me/emails",
	Summary:     "Add an unverified email address to the caller's account.",
}

var OperationSendMyEmailCode = huma.Operation{
	OperationID: "sendMyEmailCode",
	Method:      http.MethodPost,
	Path:        "/me/emails/{id}/send-code",
	Summary:     "Mail a verification code for one of the caller's addresses.",
}

var OperationVerifyMyEmail = huma.Operation{
	OperationID: "verifyMyEmail",
	Method:      http.MethodPost,
	Path:        "/me/emails/{id}/verify",
	Summary:     "Verify one of the caller's addresses with the mailed code.",
}

var OperationSetMyEmailPrimary = huma.Operation{
	OperationID: "setMyEmailPrimary",
	Method:      http.MethodPost,
	Path:        "/me/emails/{id}/primary",
	Summary:     "Make one of the caller's verified addresses the primary.",
}

var OperationDeleteMyEmail = huma.Operation{
	OperationID: "deleteMyEmail",
	Method:      http.MethodDelete,
	Path:        "/me/emails/{id}",
	Summary:     "Remove one of the caller's addresses (not the primary).",
}

var OperationListAccountEmails = huma.Operation{
	OperationID: "listAccountEmails",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/emails",
	Summary:     "List one account's email addresses (admin).",
}

var OperationAddAccountEmail = huma.Operation{
	OperationID: "addAccountEmail",
	Method:      http.MethodPost,
	Path:        "/accounts/{id}/emails",
	Summary:     "Add an address to an account, optionally asserting it verified and primary (admin).",
}

var OperationVerifyAccountEmail = huma.Operation{
	OperationID: "verifyAccountEmail",
	Method:      http.MethodPost,
	Path:        "/accounts/{id}/emails/{emailId}/verify",
	Summary:     "Assert one of an account's addresses as verified (admin).",
}

var OperationSetAccountEmailPrimary = huma.Operation{
	OperationID: "setAccountEmailPrimary",
	Method:      http.MethodPost,
	Path:        "/accounts/{id}/emails/{emailId}/primary",
	Summary:     "Make one of an account's verified addresses the primary (admin).",
}

var OperationDeleteAccountEmail = huma.Operation{
	OperationID: "deleteAccountEmail",
	Method:      http.MethodDelete,
	Path:        "/accounts/{id}/emails/{emailId}",
	Summary:     "Remove one of an account's addresses; removing the primary promotes a fallback (admin).",
}

var OperationGetMailSettings = huma.Operation{
	OperationID: "getMailSettings",
	Method:      http.MethodGet,
	Path:        "/admin/settings/mail",
	Summary:     "Report the instance's outbound-mail configuration (no credentials).",
}

var OperationSendTestMail = huma.Operation{
	OperationID: "sendTestMail",
	Method:      http.MethodPost,
	Path:        "/admin/settings/mail/test",
	Summary:     "Send a test mail through the configured relay (admin).",
}

// MailSettingsView reports the outbound-mail shape for the admin settings
// page. Username and password never leave the server.
type MailSettingsView struct {
	Configured bool   `json:"configured"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Security   string `json:"security,omitempty"`
	From       string `json:"from,omitempty"`
}
