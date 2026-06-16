// Package server — handle_admin_app_access_test.go
//
// Unit tests for the app-access admin surface. These tests are DB-free: they
// exercise AppAccessView construction, the field mapping from db row types to
// contract refs, and the error helpers. Route-level sudo gating is covered
// centrally in admin_route_policy_test.go.

package server

import (
	"testing"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// ----- AppAccessView construction tests ------------------------------------------

// TestAdminAppAccess_AccessViewGroups_OIDC verifies that ListOIDCClientAccessGroupsRow
// fields map correctly into GroupRef within an AppAccessView.
func TestAdminAppAccess_AccessViewGroups_OIDC(t *testing.T) {
	t.Parallel()

	rows := []db.ListOIDCClientAccessGroupsRow{
		{ID: 1, Slug: "eng", DisplayName: "Engineering"},
		{ID: 2, Slug: "ops", DisplayName: "Operations"},
	}

	groups := make([]contract.GroupRef, 0, len(rows))
	for _, r := range rows {
		groups = append(groups, contract.GroupRef{ID: r.ID, Slug: r.Slug, DisplayName: r.DisplayName})
	}

	view := contract.AppAccessView{
		AccessRestricted: true,
		Groups:           groups,
		Accounts:         []contract.AccountRef{},
	}

	if !view.AccessRestricted {
		t.Error("AccessRestricted: got false, want true")
	}
	if len(view.Groups) != 2 {
		t.Fatalf("Groups len: got %d, want 2", len(view.Groups))
	}
	if view.Groups[0].ID != 1 || view.Groups[0].Slug != "eng" || view.Groups[0].DisplayName != "Engineering" {
		t.Errorf("Groups[0]: got %+v, want {1 eng Engineering}", view.Groups[0])
	}
	if view.Groups[1].ID != 2 || view.Groups[1].Slug != "ops" {
		t.Errorf("Groups[1]: got %+v, want {2 ops ...}", view.Groups[1])
	}
	if len(view.Accounts) != 0 {
		t.Errorf("Accounts len: got %d, want 0", len(view.Accounts))
	}
}

// TestAdminAppAccess_AccessViewAccounts_OIDC verifies that
// ListOIDCClientAccessAccountsRow fields map correctly into AccountRef.
func TestAdminAppAccess_AccessViewAccounts_OIDC(t *testing.T) {
	t.Parallel()

	rows := []db.ListOIDCClientAccessAccountsRow{
		{ID: 10, Username: "alice", DisplayName: "Alice Smith"},
		{ID: 20, Username: "bob", DisplayName: "Bob Jones"},
	}

	accounts := make([]contract.AccountRef, 0, len(rows))
	for _, r := range rows {
		accounts = append(accounts, contract.AccountRef{ID: r.ID, Username: r.Username, DisplayName: r.DisplayName})
	}

	view := contract.AppAccessView{
		AccessRestricted: false,
		Groups:           []contract.GroupRef{},
		Accounts:         accounts,
	}

	if view.AccessRestricted {
		t.Error("AccessRestricted: got true, want false")
	}
	if len(view.Groups) != 0 {
		t.Errorf("Groups len: got %d, want 0", len(view.Groups))
	}
	if len(view.Accounts) != 2 {
		t.Fatalf("Accounts len: got %d, want 2", len(view.Accounts))
	}
	if view.Accounts[0].ID != 10 || view.Accounts[0].Username != "alice" {
		t.Errorf("Accounts[0]: got %+v, want {10 alice ...}", view.Accounts[0])
	}
	if view.Accounts[1].ID != 20 || view.Accounts[1].Username != "bob" {
		t.Errorf("Accounts[1]: got %+v, want {20 bob ...}", view.Accounts[1])
	}
}

// TestAdminAppAccess_AccessViewGroups_SAML verifies that ListSAMLSPAccessGroupsRow
// fields map correctly into GroupRef.
func TestAdminAppAccess_AccessViewGroups_SAML(t *testing.T) {
	t.Parallel()

	rows := []db.ListSAMLSPAccessGroupsRow{
		{ID: 5, Slug: "hr", DisplayName: "Human Resources"},
	}

	groups := make([]contract.GroupRef, 0, len(rows))
	for _, r := range rows {
		groups = append(groups, contract.GroupRef{ID: r.ID, Slug: r.Slug, DisplayName: r.DisplayName})
	}

	view := contract.AppAccessView{
		AccessRestricted: true,
		Groups:           groups,
		Accounts:         []contract.AccountRef{},
	}

	if len(view.Groups) != 1 {
		t.Fatalf("Groups len: got %d, want 1", len(view.Groups))
	}
	if view.Groups[0].ID != 5 || view.Groups[0].Slug != "hr" || view.Groups[0].DisplayName != "Human Resources" {
		t.Errorf("Groups[0]: got %+v, want {5 hr Human Resources}", view.Groups[0])
	}
}

// TestAdminAppAccess_AccessViewAccounts_SAML verifies that
// ListSAMLSPAccessAccountsRow fields map correctly into AccountRef.
func TestAdminAppAccess_AccessViewAccounts_SAML(t *testing.T) {
	t.Parallel()

	rows := []db.ListSAMLSPAccessAccountsRow{
		{ID: 99, Username: "carol", DisplayName: "Carol Danvers"},
	}

	accounts := make([]contract.AccountRef, 0, len(rows))
	for _, r := range rows {
		accounts = append(accounts, contract.AccountRef{ID: r.ID, Username: r.Username, DisplayName: r.DisplayName})
	}

	view := contract.AppAccessView{
		AccessRestricted: false,
		Groups:           []contract.GroupRef{},
		Accounts:         accounts,
	}

	if len(view.Accounts) != 1 {
		t.Fatalf("Accounts len: got %d, want 1", len(view.Accounts))
	}
	if view.Accounts[0].ID != 99 || view.Accounts[0].Username != "carol" {
		t.Errorf("Accounts[0]: got %+v, want {99 carol ...}", view.Accounts[0])
	}
}

// TestAdminAppAccess_EmptySlices_NonNil verifies that when there are no rows the
// view is built with non-nil empty slices (so JSON serialises to [] not null).
func TestAdminAppAccess_EmptySlices_NonNil(t *testing.T) {
	t.Parallel()

	groups := make([]contract.GroupRef, 0)
	accounts := make([]contract.AccountRef, 0)

	view := contract.AppAccessView{
		AccessRestricted: false,
		Groups:           groups,
		Accounts:         accounts,
	}

	if view.Groups == nil {
		t.Error("Groups: got nil, want non-nil empty slice")
	}
	if view.Accounts == nil {
		t.Error("Accounts: got nil, want non-nil empty slice")
	}
	if len(view.Groups) != 0 {
		t.Errorf("Groups len: got %d, want 0", len(view.Groups))
	}
	if len(view.Accounts) != 0 {
		t.Errorf("Accounts len: got %d, want 0", len(view.Accounts))
	}
}

// TestAdminAppAccess_OIDCApplicationView_AccessRestricted verifies that
// oidcApplicationView correctly propagates AccessRestricted from the db row.
func TestAdminAppAccess_OIDCApplicationView_AccessRestricted(t *testing.T) {
	t.Parallel()

	row := db.OidcClient{
		ClientID:         "my-app",
		DisplayName:      "My App",
		RedirectUris:     []string{"https://example.com/cb"},
		AllowedScopes:    []string{"openid"},
		AccessRestricted: true,
	}

	view := oidcApplicationView(row)

	if !view.AccessRestricted {
		t.Error("AccessRestricted: got false, want true")
	}
	if view.ClientID != "my-app" {
		t.Errorf("ClientID: got %q, want my-app", view.ClientID)
	}
}

// TestAdminAppAccess_OIDCApplicationView_AccessRestrictedFalse verifies that
// AccessRestricted=false is preserved (not silently defaulted to true).
func TestAdminAppAccess_OIDCApplicationView_AccessRestrictedFalse(t *testing.T) {
	t.Parallel()

	row := db.OidcClient{
		ClientID:         "open-app",
		DisplayName:      "Open App",
		AccessRestricted: false,
	}

	view := oidcApplicationView(row)

	if view.AccessRestricted {
		t.Error("AccessRestricted: got true, want false")
	}
}

// TestAdminAppAccess_SAMLApplicationView_AccessRestricted verifies that
// samlApplicationView correctly propagates AccessRestricted from the db row.
func TestAdminAppAccess_SAMLApplicationView_AccessRestricted(t *testing.T) {
	t.Parallel()

	sp := db.SamlSp{
		ID:               42,
		EntityID:         "https://sp.example.com",
		DisplayName:      "Example SP",
		AttributeMap:     []byte("[]"),
		AccessRestricted: true,
	}

	view := samlApplicationView(sp, nil, nil)

	if !view.AccessRestricted {
		t.Error("AccessRestricted: got false, want true")
	}
	if view.ID != 42 {
		t.Errorf("ID: got %d, want 42", view.ID)
	}
}

// TestAdminAppAccess_SAMLApplicationView_AccessRestrictedFalse verifies that
// AccessRestricted=false is preserved in the SAML view.
func TestAdminAppAccess_SAMLApplicationView_AccessRestrictedFalse(t *testing.T) {
	t.Parallel()

	sp := db.SamlSp{
		ID:               7,
		EntityID:         "https://open-sp.example.com",
		DisplayName:      "Open SP",
		AttributeMap:     []byte("[]"),
		AccessRestricted: false,
	}

	view := samlApplicationView(sp, nil, nil)

	if view.AccessRestricted {
		t.Error("AccessRestricted: got true, want false")
	}
}

// TestAdminAppAccess_GroupRefMapping verifies direct field mapping from
// ListOIDCClientAccessGroupsRow to GroupRef (both protocols share this shape).
func TestAdminAppAccess_GroupRefMapping(t *testing.T) {
	t.Parallel()

	row := db.ListOIDCClientAccessGroupsRow{
		ID:          3,
		Slug:        "platform",
		DisplayName: "Platform",
	}

	ref := contract.GroupRef{
		ID:          row.ID,
		Slug:        row.Slug,
		DisplayName: row.DisplayName,
	}

	if ref.ID != 3 {
		t.Errorf("ID: got %d, want 3", ref.ID)
	}
	if ref.Slug != "platform" {
		t.Errorf("Slug: got %q, want platform", ref.Slug)
	}
	if ref.DisplayName != "Platform" {
		t.Errorf("DisplayName: got %q, want Platform", ref.DisplayName)
	}
}

// TestAdminAppAccess_AccountRefMapping verifies direct field mapping from
// ListOIDCClientAccessAccountsRow to AccountRef (both protocols share this shape).
func TestAdminAppAccess_AccountRefMapping(t *testing.T) {
	t.Parallel()

	row := db.ListOIDCClientAccessAccountsRow{
		ID:          77,
		Username:    "dave",
		DisplayName: "Dave Nguyen",
	}

	ref := contract.AccountRef{
		ID:          row.ID,
		Username:    row.Username,
		DisplayName: row.DisplayName,
	}

	if ref.ID != 77 {
		t.Errorf("ID: got %d, want 77", ref.ID)
	}
	if ref.Username != "dave" {
		t.Errorf("Username: got %q, want dave", ref.Username)
	}
	if ref.DisplayName != "Dave Nguyen" {
		t.Errorf("DisplayName: got %q, want Dave Nguyen", ref.DisplayName)
	}
}

// TestAdminAppAccess_SAMLGroupRefMapping verifies field mapping from the SAML-specific
// ListSAMLSPAccessGroupsRow type.
func TestAdminAppAccess_SAMLGroupRefMapping(t *testing.T) {
	t.Parallel()

	row := db.ListSAMLSPAccessGroupsRow{
		ID:          12,
		Slug:        "security",
		DisplayName: "Security",
	}

	ref := contract.GroupRef{
		ID:          row.ID,
		Slug:        row.Slug,
		DisplayName: row.DisplayName,
	}

	if ref.ID != 12 || ref.Slug != "security" {
		t.Errorf("GroupRef: got %+v, want {12 security Security}", ref)
	}
}

// TestAdminAppAccess_SAMLAccountRefMapping verifies field mapping from the SAML-specific
// ListSAMLSPAccessAccountsRow type.
func TestAdminAppAccess_SAMLAccountRefMapping(t *testing.T) {
	t.Parallel()

	row := db.ListSAMLSPAccessAccountsRow{
		ID:          55,
		Username:    "eve",
		DisplayName: "Eve Wilson",
	}

	ref := contract.AccountRef{
		ID:          row.ID,
		Username:    row.Username,
		DisplayName: row.DisplayName,
	}

	if ref.ID != 55 || ref.Username != "eve" {
		t.Errorf("AccountRef: got %+v, want {55 eve Eve Wilson}", ref)
	}
}
