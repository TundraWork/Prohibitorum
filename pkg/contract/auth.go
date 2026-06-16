package contract

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// AuthKind discriminates the AuthRequirement variants.
type AuthKind uint8

const (
	AuthPublic AuthKind = iota
	AuthSession
	AuthAdmin
)

// AuthRequirement declares what authentication / authorization a Huma operation
// expects. Producers attach one per operation at registerOp() call sites;
// the registerOp helper installs a per-operation middleware that calls
// auth.Check(session, requirement) before invoking the handler.
type AuthRequirement struct {
	Kind AuthKind
}

// View types --------------------------------------------------------------------

// SessionView is the response body of GET /me — the public face of the current session.
type SessionView struct {
	ID            int32          `json:"id"`
	Username      string         `json:"username"`
	DisplayName   string         `json:"displayName"`
	Role          string         `json:"role"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	AvatarURL        *string           `json:"avatarUrl,omitempty"`
	AvatarPending    bool              `json:"avatarPending,omitempty"`
	AvatarSource     *string           `json:"avatarSource,omitempty"`
	AvatarSourceUrls map[string]string `json:"avatarSourceUrls,omitempty"`
}

// AccountView is admin-facing; lastSignInAt is derived from the account's credentials.
type AccountView struct {
	ID            int32          `json:"id"`
	Username      string         `json:"username"`
	DisplayName   string         `json:"displayName"`
	Email         *string        `json:"email,omitempty"`
	EmailVerified bool           `json:"emailVerified"`
	Role          string         `json:"role"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Disabled      bool           `json:"disabled"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	LastSignInAt  *time.Time     `json:"lastSignInAt,omitempty"`
	AvatarURL     *string        `json:"avatarUrl,omitempty"`
}

// SessionListItem is a single row in /me/sessions. Token is intentionally
// not exposed — id is an opaque, non-secret handle generated at session
// issuance, safe to round-trip from /me/sessions/revoke.
type SessionListItem struct {
	ID         string    `json:"id"`
	IsCurrent  bool      `json:"isCurrent"`
	IssuedAt   time.Time `json:"issuedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenIP string    `json:"lastSeenIp"`
	UserAgent  string    `json:"userAgent,omitempty"`
}

// CredentialView is shown on /me and on admin views of a specific account.
// CredentialIDSuffix is the last 4 chars of base64url(credential_id), for forensic
// display only; the full credential ID is never returned in API responses.
type CredentialView struct {
	ID                 int32      `json:"id"`
	CredentialIDSuffix string     `json:"credentialIdSuffix"`
	Nickname           *string    `json:"nickname,omitempty"`
	Transports         []string   `json:"transports"`
	BackupState        bool       `json:"backupState"`
	AttestationType    string     `json:"attestationType"`
	CreatedAt          time.Time  `json:"createdAt"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
}

// EnrollmentTarget is the public-safe identity of the target account for invite/reset.
// Bootstrap intents have no target; the field is omitted in that case.
type EnrollmentTarget struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// EnrollmentPreview is the response of GET /enrollments/{token} — what the
// enroll page needs to render the right form before triggering the ceremony.
type EnrollmentPreview struct {
	Intent    string            `json:"intent"`
	Target    *EnrollmentTarget `json:"target,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// AuthStatus is GET /auth/status — used by the dashboard LoginView to branch
// between the "Sign in with passkey" button and the "Run prohibitorum enroll-admin"
// instruction.
type AuthStatus struct {
	Bootstrapped bool `json:"bootstrapped"`
}

// EnrollmentURLResponse is returned by reissue-enrollment. Reveal-once: the URL
// is never retrievable from any other endpoint after this response.
type EnrollmentURLResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// InvitationResponse is returned by POST /invitations. The account does NOT
// exist yet — it's created when the invitee consumes the enrollment URL.
// The URL also appears in GET /invitations (listInvitations) so admins can
// retrieve it again without reissuing.
type InvitationResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Operations --------------------------------------------------------------------
// Paths are under /api/prohibitorum; the prefix is added by huma at registration time.

var OperationAuthStatus = huma.Operation{
	OperationID: "getAuthStatus",
	Method:      http.MethodGet,
	Path:        "/auth/status",
	Summary:     "Whether at least one non-disabled admin account exists.",
}

var OperationLoginBegin = huma.Operation{
	OperationID: "beginLogin",
	Method:      http.MethodPost,
	Path:        "/auth/login/begin",
	Summary:     "Start a discoverable-credential login ceremony.",
}

var OperationLoginComplete = huma.Operation{
	OperationID: "completeLogin",
	Method:      http.MethodPost,
	Path:        "/auth/login/complete",
	Summary:     "Finish the login ceremony and issue a session.",
}

var OperationLogout = huma.Operation{
	OperationID: "logout",
	Method:      http.MethodPost,
	Path:        "/auth/logout",
	Summary:     "Revoke the current session (idempotent).",
}

var OperationGetMe = huma.Operation{
	OperationID: "getMe",
	Method:      http.MethodGet,
	Path:        "/me",
	Summary:     "Return the authenticated session view.",
}

// MeFactorsView is the response body of GET /me/factors — the caller's
// enrolled sign-in factor status as of the current moment.
type MeFactorsView struct {
	PasswordSet            bool `json:"passwordSet"`
	TOTPEnrolled           bool `json:"totpEnrolled"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
	PasskeyCount           int  `json:"passkeyCount"`
}

var OperationGetMyFactors = huma.Operation{
	OperationID: "getMyFactors",
	Method:      http.MethodGet,
	Path:        "/me/factors",
	Summary:     "Return the caller's enrolled sign-in factor status.",
}

var OperationUpdateMe = huma.Operation{
	OperationID: "updateMe",
	Method:      http.MethodPut,
	Path:        "/me",
	Summary:     "Update the caller's own profile (display name only).",
}

var OperationListMyCredentials = huma.Operation{
	OperationID: "listMyCredentials",
	Method:      http.MethodGet,
	Path:        "/me/credentials",
	Summary:     "List the caller's registered passkeys.",
}

var OperationAddCredentialBegin = huma.Operation{
	OperationID: "beginAddCredential",
	Method:      http.MethodPost,
	Path:        "/me/credentials/register/begin",
	Summary:     "Start a registration ceremony to add another passkey to the current account.",
}

var OperationAddCredentialComplete = huma.Operation{
	OperationID: "completeAddCredential",
	Method:      http.MethodPost,
	Path:        "/me/credentials/register/complete",
	Summary:     "Finish a passkey-addition ceremony.",
}

var OperationDeleteMyCredential = huma.Operation{
	OperationID: "deleteMyCredential",
	Method:      http.MethodPost,
	Path:        "/me/credentials/delete",
	Summary:     "Remove one of the caller's passkeys (rejected when it would leave zero).",
}

var OperationRenameMyCredential = huma.Operation{
	OperationID: "renameMyCredential",
	Method:      http.MethodPost,
	Path:        "/me/credentials/rename",
	Summary:     "Rename one of the caller's own passkeys.",
}

var OperationListMySessions = huma.Operation{
	OperationID: "listMySessions",
	Method:      http.MethodGet,
	Path:        "/me/sessions",
	Summary:     "List the caller's active sessions across devices.",
}

var OperationRevokeMySession = huma.Operation{
	OperationID: "revokeMySession",
	Method:      http.MethodPost,
	Path:        "/me/sessions/revoke",
	Summary:     "Revoke one of the caller's sessions by id; cannot target the current session.",
}

var OperationPreviewEnrollment = huma.Operation{
	OperationID: "previewEnrollment",
	Method:      http.MethodGet,
	Path:        "/enrollments/{token}",
	Summary:     "Read the metadata of an enrollment token so the UI can render the right form.",
}

var OperationEnrollmentRegisterBegin = huma.Operation{
	OperationID: "beginEnrollmentRegistration",
	Method:      http.MethodPost,
	Path:        "/enrollments/{token}/register/begin",
	Summary:     "Start the registration ceremony driven by an enrollment token.",
}

var OperationEnrollmentRegisterComplete = huma.Operation{
	OperationID: "completeEnrollmentRegistration",
	Method:      http.MethodPost,
	Path:        "/enrollments/{token}/register/complete",
	Summary:     "Finish the enrollment-driven registration ceremony.",
}

var OperationListAccounts = huma.Operation{
	OperationID: "listAccounts",
	Method:      http.MethodGet,
	Path:        "/accounts",
	Summary:     "List all accounts (admin only).",
}

var OperationGetAccount = huma.Operation{
	OperationID: "getAccount",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}",
	Summary:     "Get one account by id (admin only).",
}

var OperationUpdateAccount = huma.Operation{
	OperationID: "updateAccount",
	Method:      http.MethodPut,
	Path:        "/accounts/{id}",
	Summary:     "Update display name, role, attributes, or disabled flag on an account.",
}

var OperationDeleteAccount = huma.Operation{
	OperationID: "deleteAccount",
	Method:      http.MethodPost,
	Path:        "/accounts/delete",
	Summary:     "Hard-delete an account.",
}

var OperationListAccountCredentials = huma.Operation{
	OperationID: "listAccountCredentials",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/credentials",
	Summary:     "List an account's WebAuthn credentials (admin only).",
}

var OperationListAccountSessions = huma.Operation{
	OperationID: "listAccountSessions",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/sessions",
	Summary:     "List an account's active sessions (admin only).",
}

var OperationListAccountGroups = huma.Operation{
	OperationID: "listAccountGroups",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/groups",
	Summary:     "List the groups an account belongs to (admin only).",
}

var OperationRevokeAccountSessions = huma.Operation{
	OperationID: "revokeAccountSessions",
	Method:      http.MethodPost,
	Path:        "/accounts/revoke-sessions",
	Summary:     "Kick all active sessions for an account.",
}

var OperationReissueEnrollment = huma.Operation{
	OperationID: "reissueEnrollment",
	Method:      http.MethodPost,
	Path:        "/accounts/reissue-enrollment",
	Summary:     "Issue a reset-intent enrollment URL for an account; reveal-once.",
}

var OperationCreateInvitation = huma.Operation{
	OperationID: "createInvitation",
	Method:      http.MethodPost,
	Path:        "/invitations",
	Summary:     "Create an invite-intent enrollment URL. The new account is created when the invitee consumes the URL.",
}

// InvitationView is the server-side projection of a pending enrollment row,
// including the URL so admin clients don't have to reconstruct it.
type InvitationView struct {
	Token                   string         `json:"token"`
	URL                     string         `json:"url"`
	Role                    string         `json:"role"`
	Attributes              map[string]any `json:"attributes,omitempty"`
	ExpectedUpstreamIdpSlug *string        `json:"expectedUpstreamIdpSlug,omitempty"`
	CreatedAt               time.Time      `json:"createdAt"`
	ExpiresAt               time.Time      `json:"expiresAt"`
}

var OperationListInvitations = huma.Operation{
	OperationID: "listInvitations",
	Method:      http.MethodGet,
	Path:        "/invitations",
	Summary:     "List outstanding (unconsumed, unexpired) invitations.",
}

var OperationRevokeInvitation = huma.Operation{
	OperationID: "revokeInvitation",
	Method:      http.MethodPost,
	Path:        "/invitations/revoke",
	Summary:     "Revoke a pending invitation by token.",
}

// ConsentContext is GET /api/prohibitorum/consent — the data the consent UI
// needs to render. Scope *descriptions* are owned by the frontend i18n layer.
type ConsentContext struct {
	Client  ConsentClient `json:"client"`
	Account ConsentUser   `json:"account"`
	Scopes  []string      `json:"scopes"`
}

type ConsentClient struct {
	ClientID    string `json:"clientId"`
	DisplayName string `json:"displayName"`
	LogoURI     string `json:"logoUri,omitempty"`
	PolicyURI   string `json:"policyUri,omitempty"`
	TosURI      string `json:"tosUri,omitempty"`
}

type ConsentUser struct {
	DisplayName string `json:"displayName"`
}

// ConsentDecision is the POST body. Decision is "approve" or "deny".
type ConsentDecision struct {
	Ticket   string `json:"ticket"`
	Decision string `json:"decision"`
}

// ConsentResult tells the SPA where to navigate next.
type ConsentResult struct {
	Redirect string `json:"redirect"`
}

// FederationProvider is one entry in GET /api/prohibitorum/auth/federation.
type FederationProvider struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

// FederationConfirmView is the /welcome confirmation-step projection returned by
// GET /api/prohibitorum/auth/federation/confirm. It surfaces the pending
// identity (resolved via a single-use, browser-bound confirmation grant) so the
// user can confirm or decline a first-time federated sign-in. AvatarURL is the
// already-stored avatar (nil while still fetching); AvatarPending reports
// whether the background upstream-avatar inherit is still in flight.
type FederationConfirmView struct {
	IDPDisplayName string  `json:"idpDisplayName"`
	DisplayName    string  `json:"displayName"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	AvatarURL      *string `json:"avatarUrl,omitempty"`
	AvatarPending  bool    `json:"avatarPending"`
}

// SigningKeyView is the admin-facing projection of a signing_key row.
// Private key material is NEVER included — only the public JWK and lifecycle
// timestamps are returned to callers.
type SigningKeyView struct {
	Kid              string         `json:"kid"`
	Algorithm        string         `json:"algorithm"`
	Use              string         `json:"use"`
	Status           string         `json:"status"`
	PublicJWK        map[string]any `json:"publicJwk"`
	ActivatedAt      *time.Time     `json:"activatedAt,omitempty"`
	DecommissionedAt *time.Time     `json:"decommissionedAt,omitempty"`
	RetireAfter      *time.Time     `json:"retireAfter,omitempty"`
}

var OperationListSigningKeys = huma.Operation{
	OperationID: "listSigningKeys",
	Method:      http.MethodGet,
	Path:        "/signing-keys",
	Summary:     "List signing keys with lifecycle status (admin only). Private material is never returned.",
}

// OIDCApplicationView is the admin-facing projection of an oidc_client row.
// client_secret_hash is NEVER included — only the public configuration fields
// are returned to callers.
type OIDCApplicationView struct {
	ClientID                string    `json:"clientId"`
	DisplayName             string    `json:"displayName"`
	RedirectURIs            []string  `json:"redirectUris"`
	PostLogoutRedirectURIs  []string  `json:"postLogoutRedirectUris"`
	AllowedScopes           []string  `json:"allowedScopes"`
	TokenEndpointAuthMethod string    `json:"tokenEndpointAuthMethod"`
	RequireConsent          bool      `json:"requireConsent"`
	Disabled                bool      `json:"disabled"`
	AccessRestricted        bool      `json:"accessRestricted"`
	CreatedAt               time.Time `json:"createdAt"`
}

var OperationListOIDCApplications = huma.Operation{
	OperationID: "listOIDCApplications",
	Method:      http.MethodGet,
	Path:        "/oidc-applications",
	Summary:     "List all OIDC applications (admin only). Secret material is never returned.",
}

var OperationGetOIDCApplication = huma.Operation{
	OperationID: "getOIDCApplication",
	Method:      http.MethodGet,
	Path:        "/oidc-applications/{clientId}",
	Summary:     "Get one OIDC application by client_id (admin only). Secret material is never returned.",
}

// SAMLACSView is the wire representation of a single AssertionConsumerService
// endpoint registered for a SAML SP.
type SAMLACSView struct {
	Binding   string `json:"binding"`
	Location  string `json:"location"`
	Index     int32  `json:"index"`
	IsDefault bool   `json:"isDefault"`
}

// SAMLKeyView is the wire representation of a signing/encryption key
// certificate registered for a SAML SP. Raw PEM is not returned — callers
// receive a fingerprint-suitable summary (notAfter and use) only.
type SAMLKeyView struct {
	Use      string     `json:"use"`
	NotAfter *time.Time `json:"notAfter,omitempty"`
}

// SAMLApplicationView is the admin-facing projection of a saml_sp row plus its
// associated ACS endpoints and key summaries. Raw certificate material (PEM)
// is never returned — SAMLKeyView carries only the lifecycle fields.
type SAMLApplicationView struct {
	ID                        int64           `json:"id"`
	EntityID                  string          `json:"entityId"`
	DisplayName               string          `json:"displayName"`
	Kind                      string          `json:"kind,omitempty"`
	NameIDFormat              string          `json:"nameIdFormat"`
	AttributeMap              json.RawMessage `json:"attributeMap"`
	RequireSignedAuthnRequest bool            `json:"requireSignedAuthnRequest"`
	AllowIdpInitiated         bool            `json:"allowIdpInitiated"`
	Disabled                  bool            `json:"disabled"`
	AccessRestricted          bool            `json:"accessRestricted"`
	SessionLifetimeSecs       *int64          `json:"sessionLifetimeSecs,omitempty"`
	ACS                       []SAMLACSView   `json:"acs"`
	Keys                      []SAMLKeyView   `json:"keys"`
	CreatedAt                 time.Time       `json:"createdAt"`
}

// IdentityProviderView is the admin-facing projection of an upstream_idp row.
// client_secret_enc and secret_nonce are NEVER included — the sealed bytes
// are write-only. Only the public configuration fields are returned.
type IdentityProviderView struct {
	Slug                 string    `json:"slug"`
	DisplayName          string    `json:"displayName"`
	IssuerUrl            string    `json:"issuerUrl"`
	ClientID             string    `json:"clientId"`
	Scopes               []string  `json:"scopes"`
	Mode                 string    `json:"mode"`
	AllowedDomains       []string  `json:"allowedDomains"`
	UsernameClaim        string    `json:"usernameClaim"`
	DisplayNameClaim     string    `json:"displayNameClaim"`
	EmailClaim           string    `json:"emailClaim"`
	PictureClaim         string    `json:"pictureClaim"`
	RequireVerifiedEmail bool      `json:"requireVerifiedEmail"`
	Disabled             bool      `json:"disabled"`
	CreatedAt            time.Time `json:"createdAt"`
}

var OperationListIdentityProviders = huma.Operation{
	OperationID: "listIdentityProviders",
	Method:      http.MethodGet,
	Path:        "/identity-providers",
	Summary:     "List all identity providers including disabled (admin only). Secret material is never returned.",
}

var OperationGetIdentityProvider = huma.Operation{
	OperationID: "getIdentityProvider",
	Method:      http.MethodGet,
	Path:        "/identity-providers/{slug}",
	Summary:     "Get one identity provider by slug including disabled (admin only). Secret material is never returned.",
}

var OperationListSAMLApplications = huma.Operation{
	OperationID: "listSAMLApplications",
	Method:      http.MethodGet,
	Path:        "/saml-applications",
	Summary:     "List all SAML applications (admin only). Certificate PEM is never returned.",
}

var OperationGetSAMLApplication = huma.Operation{
	OperationID: "getSAMLApplication",
	Method:      http.MethodGet,
	Path:        "/saml-applications/{id}",
	Summary:     "Get one SAML application by id (admin only).",
}

// AuditEventView is the admin-facing projection of a credential_event row.
// The detail column is passed through as-is from the emitting mutation handler;
// this viewer does NOT redact — redaction is a write-site invariant enforced by
// the handlers that call audit.Writer.Record (Tasks 3-6). The credential_event
// table has no column that carries private key material, client secrets, tokens,
// or auth codes — those are design-level invariants of the schema.
type AuditEventView struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	AccountID *int32         `json:"accountId,omitempty"`
	Factor    string         `json:"factor"`
	Event     string         `json:"event"`
	IP        string         `json:"ip,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

var OperationListAuditEvents = huma.Operation{
	OperationID: "listAuditEvents",
	Method:      http.MethodGet,
	Path:        "/audit-events",
	Summary:     "List credential/admin audit events, newest first, with filters and keyset pagination (admin only).",
}

// GroupView is the admin-facing projection of a user_group row.
// MemberCount is included on the list endpoint; it is omitted (zero) on
// the single-get endpoint (which can be fetched separately via the members list).
type GroupView struct {
	ID                  int32     `json:"id"`
	Slug                string    `json:"slug"`
	DisplayName         string    `json:"displayName"`
	Description         string    `json:"description,omitempty"`
	ExposedToDownstream bool      `json:"exposedToDownstream"`
	MemberCount         int64     `json:"memberCount,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

// GroupMemberView is a single row in GET /groups/{id}/members.
type GroupMemberView struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// GroupRef is a compact reference to a group, reused by access-control and
// downstream-claims tasks.
type GroupRef struct {
	ID          int32  `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

// AccountRef is a compact reference to an account, reused by access-control
// and membership tasks.
type AccountRef struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

var OperationListGroups = huma.Operation{
	OperationID: "listGroups",
	Method:      http.MethodGet,
	Path:        "/groups",
	Summary:     "List all groups with member counts (admin only).",
}

var OperationGetGroup = huma.Operation{
	OperationID: "getGroup",
	Method:      http.MethodGet,
	Path:        "/groups/{id}",
	Summary:     "Get one group by id (admin only).",
}

var OperationListGroupMembers = huma.Operation{
	OperationID: "listGroupMembers",
	Method:      http.MethodGet,
	Path:        "/groups/{id}/members",
	Summary:     "List members of a group (admin only).",
}

// AppAccessView is the response body for the GET …/access endpoints. It
// combines the access_restricted flag with the lists of groups and accounts
// that have been explicitly granted access to the application.
type AppAccessView struct {
	AccessRestricted bool         `json:"accessRestricted"`
	Groups           []GroupRef   `json:"groups"`
	Accounts         []AccountRef `json:"accounts"`
}

var OperationGetOIDCClientAccess = huma.Operation{
	OperationID: "getOIDCClientAccess",
	Method:      http.MethodGet,
	Path:        "/oidc-applications/{clientId}/access",
	Summary:     "Get access restriction status and granted principals for an OIDC application (admin only).",
}

var OperationGetSAMLSPAccess = huma.Operation{
	OperationID: "getSAMLSPAccess",
	Method:      http.MethodGet,
	Path:        "/saml-applications/{id}/access",
	Summary:     "Get access restriction status and granted principals for a SAML application (admin only).",
}
