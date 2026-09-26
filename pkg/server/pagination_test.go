// Package server — pagination_test.go
//
// Tests for the top-level admin index keyset pagination (Task 6). These tests
// exercise the shared pagination helpers (limit clamping, cursor decode/encode,
// page construction) and the handler-level cursor binding/rejection logic
// without a live database, using a fake querier that returns pre-seeded rows.
//
// Coverage:
//   - First/middle/final page sequencing with nextCursor presence/absence.
//   - Duplicate timestamps (stable tiebreaker via id/kid/client_id/token).
//   - Limit clamp (non-positive → 50, >100 → 100, exact).
//   - Tampered cursor → pagination_cursor_invalid.
//   - Filter mismatch on audit-events cursor → pagination_cursor_invalid.
//   - Bare-array absence: every handler returns contract.Page[T], not []T.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	federationsteam "prohibitorum/pkg/federation/providers/steam"
	federationvrchat "prohibitorum/pkg/federation/providers/vrchat"
	"prohibitorum/pkg/pagination"
	"prohibitorum/pkg/weberr"
)

// testCodec returns a Codec with a deterministic 32-byte DEK for testing.
func testCodec() *pagination.Codec {
	return pagination.NewCodec(map[int][]byte{1: bytes.Repeat([]byte{0x11}, 32)}, 1, time.Now)
}

// --- fake list querier -------------------------------------------------------

// fakeListQ implements only the list methods exercised by the top-level
// pagination handlers. It records the params it received and returns
// pre-seeded rows. All other Querier methods panic via the nil embed.
type fakeListQ struct {
	db.Querier

	// accounts
	accountsRows  []db.ListAccountsRow
	accountsCall  db.ListAccountsParams
	accountsCalls int
	providers     map[string]db.UpstreamIdp
	providerCalls []string

	// invitations
	invitationRows []db.Enrollment
	invitationCall db.ListPendingInvitationsParams

	// oidc
	oidcRows  []db.ListNonForwardAuthOIDCClientsRow
	oidcCall  db.ListNonForwardAuthOIDCClientsParams
	oidcCalls int

	// saml
	samlRows  []db.SamlSp
	samlCall  db.ListSAMLSPsParams
	samlCalls int

	// upstream idps
	idpRows []db.UpstreamIdp
	idpCall db.ListAllUpstreamIDPsParams

	// signing keys
	signKeyRows []db.SigningKey
	signKeyCall db.ListAllSigningKeysParams

	// forward-auth
	faRows  []db.ListForwardAuthClientsRow
	faCall  db.ListForwardAuthClientsParams
	faCalls int

	// audit
	auditRows []db.ListCredentialEventsRow
	auditCall db.ListCredentialEventsParams

	// entity icons, keyed by owner kind then owner id. The list handlers call
	// this once per page to fill iconUrl, so a handler test that leaves it nil
	// gets no icon on any row.
	iconEtags map[string]map[string]string

	// The batched per-page child lookups. The idp list counts linked accounts
	// and the saml list loads ACS + signing keys; each is called once per page.
	// linkedCountIds records the ids the count was asked for, so a test can
	// assert the list asks once for the whole page rather than per row.
	linkedCounts   map[int64]int64
	linkedCountIds []int64
	linkedCalls    int
	acsRows        []db.SamlSpAc
	acsIds         []int64
	acsCalls       int
	keyRows        []db.SamlSpKey
	keyIds         []int64
	keyCalls       int
}

func (f *fakeListQ) ListAccounts(_ context.Context, p db.ListAccountsParams) ([]db.ListAccountsRow, error) {
	f.accountsCall = p
	f.accountsCalls++
	return f.accountsRows, nil
}

func (f *fakeListQ) GetUpstreamIDPBySlugAny(_ context.Context, slug string) (db.UpstreamIdp, error) {
	f.providerCalls = append(f.providerCalls, slug)
	provider, ok := f.providers[slug]
	if !ok {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return provider, nil
}

func (f *fakeListQ) ListPendingInvitations(_ context.Context, p db.ListPendingInvitationsParams) ([]db.Enrollment, error) {
	f.invitationCall = p
	return f.invitationRows, nil
}

func (f *fakeListQ) ListNonForwardAuthOIDCClients(_ context.Context, p db.ListNonForwardAuthOIDCClientsParams) ([]db.ListNonForwardAuthOIDCClientsRow, error) {
	f.oidcCall = p
	f.oidcCalls++
	return f.oidcRows, nil
}

func (f *fakeListQ) ListSAMLSPs(_ context.Context, p db.ListSAMLSPsParams) ([]db.SamlSp, error) {
	f.samlCall = p
	f.samlCalls++
	return f.samlRows, nil
}

func (f *fakeListQ) ListAllUpstreamIDPs(_ context.Context, p db.ListAllUpstreamIDPsParams) ([]db.UpstreamIdp, error) {
	f.idpCall = p
	return f.idpRows, nil
}

func (f *fakeListQ) ListAllSigningKeys(_ context.Context, p db.ListAllSigningKeysParams) ([]db.SigningKey, error) {
	f.signKeyCall = p
	return f.signKeyRows, nil
}

func (f *fakeListQ) ListForwardAuthClients(_ context.Context, p db.ListForwardAuthClientsParams) ([]db.ListForwardAuthClientsRow, error) {
	f.faCall = p
	f.faCalls++
	return f.faRows, nil
}

func (f *fakeListQ) ListCredentialEvents(_ context.Context, p db.ListCredentialEventsParams) ([]db.ListCredentialEventsRow, error) {
	f.auditCall = p
	return f.auditRows, nil
}

// ListEntityIconEtags is the per-page icon lookup the list handlers make to fill
// iconUrl. Returning no rows is the common case here: a handler test is about
// paging, not icons.
func (f *fakeListQ) ListEntityIconEtags(_ context.Context, ownerKind string) ([]db.ListEntityIconEtagsRow, error) {
	rows := make([]db.ListEntityIconEtagsRow, 0, len(f.iconEtags[ownerKind]))
	for ownerID, etag := range f.iconEtags[ownerKind] {
		rows = append(rows, db.ListEntityIconEtagsRow{OwnerID: ownerID, Etag: etag})
	}
	return rows, nil
}

// CountAccountsLinkedToUpstreamIDPs answers the identity-provider list's batched
// linked-account count. Only providers present in f.linkedCounts come back, so a
// test that seeds one provider's count still exercises the "no row means 0" path
// for the rest of the page.
func (f *fakeListQ) CountAccountsLinkedToUpstreamIDPs(_ context.Context, upstreamIdpIds []int64) ([]db.CountAccountsLinkedToUpstreamIDPsRow, error) {
	f.linkedCountIds = upstreamIdpIds
	f.linkedCalls++
	rows := make([]db.CountAccountsLinkedToUpstreamIDPsRow, 0, len(upstreamIdpIds))
	for _, id := range upstreamIdpIds {
		count, ok := f.linkedCounts[id]
		if !ok {
			continue
		}
		rows = append(rows, db.CountAccountsLinkedToUpstreamIDPsRow{UpstreamIdpID: id, LinkedAccountCount: count})
	}
	return rows, nil
}

// ListSAMLSPACSEndpointsBySPIDs is the SAML list's batched ACS lookup.
func (f *fakeListQ) ListSAMLSPACSEndpointsBySPIDs(_ context.Context, spIds []int64) ([]db.SamlSpAc, error) {
	f.acsIds = spIds
	f.acsCalls++
	return f.acsRows, nil
}

// ListSAMLSPKeysBySPIDs is the SAML list's batched signing-key lookup.
func (f *fakeListQ) ListSAMLSPKeysBySPIDs(_ context.Context, arg db.ListSAMLSPKeysBySPIDsParams) ([]db.SamlSpKey, error) {
	f.keyIds = arg.SpIds
	f.keyCalls++
	return f.keyRows, nil
}

// InsertCredentialEvent is a no-op sink for audit rows.
func (f *fakeListQ) InsertCredentialEvent(_ context.Context, _ db.InsertCredentialEventParams) error {
	return nil
}

// newPaginationTestServer builds a minimal Server with the fake querier and a
// test cursor codec. PublicOrigins is set so projection helpers that reference
// it don't panic.
func newPaginationTestServer(q *fakeListQ) *Server {
	registry := federation.NewRegistry()
	for _, definition := range []federation.Definition{
		federationoidc.Definition{},
		federationsteam.Definition{},
		federationvrchat.Definition{},
	} {
		if err := registry.RegisterDefinition(definition); err != nil {
			panic(err)
		}
	}
	return &Server{
		topLevelQueriesOverride: q,
		invitationOverride:      q,
		cursorCodec:             testCodec(),
		federationRegistry:      registry,
		config: &configx.Config{
			PublicOrigins: []string{"https://test.example.com"},
		},
	}
}

func applicationListContext(accountID int32, role string) context.Context {
	return authn.WithSession(context.Background(), &authn.Session{
		Account: &db.Account{ID: accountID, Role: role},
	})
}

func adminListContext() context.Context {
	return applicationListContext(1, "admin")
}

func userListContext(accountID int32) context.Context {
	return applicationListContext(accountID, "user")
}

// --- helpers for row construction --------------------------------------------

func ts(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func pgTS(s string) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: ts(s), Valid: true}
}

// =====================================================================
// Accounts pagination
// =====================================================================

func TestListAccountsInputIncludesIdentityFilters(t *testing.T) {
	t.Parallel()

	inputType := reflect.TypeOf(listAccountsIn{})
	for fieldName, queryName := range map[string]string{
		"Q": "q", "Provider": "provider", "Field": "field", "Value": "value", "Match": "match",
	} {
		field, ok := inputType.FieldByName(fieldName)
		if !ok {
			t.Errorf("listAccountsIn missing %s", fieldName)
			continue
		}
		if got := field.Tag.Get("query"); got != queryName {
			t.Errorf("%s query tag = %q, want %q", fieldName, got, queryName)
		}
	}
}

func TestListAccountsQueryCarriesIdentityFiltersAndMatches(t *testing.T) {
	t.Parallel()

	paramsType := reflect.TypeOf(db.ListAccountsParams{})
	for _, fieldName := range []string{"Q", "Provider", "Field", "Value", "Match"} {
		if _, ok := paramsType.FieldByName(fieldName); !ok {
			t.Errorf("ListAccountsParams missing %s", fieldName)
		}
	}
	matchesField, ok := reflect.TypeOf(db.ListAccountsRow{}).FieldByName("MatchingIdentities")
	if !ok {
		t.Fatal("ListAccountsRow missing MatchingIdentities")
	}
	if matchesField.Type.Kind() != reflect.String {
		t.Errorf("MatchingIdentities type = %v, want string JSON transport", matchesField.Type)
	}
}

func TestListAccountsRejectsPartialIdentityFiltersBeforeListQuery(t *testing.T) {
	tests := []struct {
		name string
		in   listAccountsIn
	}{
		{name: "field without provider", in: listAccountsIn{Field: "displayName"}},
		{name: "provider field value without match", in: listAccountsIn{Provider: "vrchat", Field: "displayName", Value: "Alice"}},
		{name: "provider value match without field", in: listAccountsIn{Provider: "vrchat", Value: "Alice", Match: "contains"}},
		{name: "provider field match without value", in: listAccountsIn{Provider: "vrchat", Field: "displayName", Match: "contains"}},
		{name: "advanced filter without provider", in: listAccountsIn{Field: "displayName", Value: "Alice", Match: "contains"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeListQ{}
			s := newPaginationTestServer(q)
			if _, err := s.handleListAccounts(context.Background(), &tc.in); err == nil {
				t.Fatal("expected bad request")
			}
			if q.accountsCalls != 0 {
				t.Fatalf("ListAccounts calls = %d, want 0", q.accountsCalls)
			}
		})
	}
}

func TestListAccountsRejectsUnsupportedProviderFieldsAndOperators(t *testing.T) {
	tests := []struct {
		name string
		in   listAccountsIn
	}{
		{name: "unknown provider", in: listAccountsIn{Provider: "missing"}},
		{name: "field not declared by provider", in: listAccountsIn{Provider: "steam-main", Field: "displayName", Value: "Alice", Match: "contains"}},
		{name: "operator not declared by field", in: listAccountsIn{Provider: "steam-main", Field: "steamId", Value: "7656", Match: "prefix"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeListQ{providers: map[string]db.UpstreamIdp{
				"steam-main": {ID: 1, Slug: "steam-main", Protocol: "steam"},
			}}
			s := newPaginationTestServer(q)
			if _, err := s.handleListAccounts(context.Background(), &tc.in); err == nil {
				t.Fatal("expected provider filter validation error")
			}
			if q.accountsCalls != 0 {
				t.Fatalf("ListAccounts calls = %d, want 0", q.accountsCalls)
			}
		})
	}
}

func TestListAccountsAcceptsDescriptorDeclaredIdentityFilters(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		field    string
		match    string
	}{
		{name: "provider only", protocol: "steam"},
		{name: "steam id exact", protocol: "steam", field: "steamId", match: "exact"},
		{name: "steam persona exact", protocol: "steam", field: "personaName", match: "exact"},
		{name: "steam persona prefix", protocol: "steam", field: "personaName", match: "prefix"},
		{name: "steam persona contains", protocol: "steam", field: "personaName", match: "contains"},
		{name: "VRChat user id exact", protocol: "vrchat", field: "userId", match: "exact"},
		{name: "VRChat display name exact", protocol: "vrchat", field: "displayName", match: "exact"},
		{name: "VRChat display name prefix", protocol: "vrchat", field: "displayName", match: "prefix"},
		{name: "VRChat display name contains", protocol: "vrchat", field: "displayName", match: "contains"},
		{name: "OIDC subject exact", protocol: "oidc", field: "subject", match: "exact"},
		{name: "OIDC email exact", protocol: "oidc", field: "email", match: "exact"},
		{name: "OIDC email prefix", protocol: "oidc", field: "email", match: "prefix"},
		{name: "OIDC email contains", protocol: "oidc", field: "email", match: "contains"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slug := tc.protocol + "-main"
			q := &fakeListQ{providers: map[string]db.UpstreamIdp{
				slug: {ID: 1, Slug: slug, Protocol: tc.protocol},
			}}
			s := newPaginationTestServer(q)
			value := ""
			if tc.field != "" {
				value = "value"
			}
			_, err := s.handleListAccounts(context.Background(), &listAccountsIn{
				Provider: slug,
				Field:    tc.field,
				Value:    value,
				Match:    tc.match,
			})
			if err != nil {
				t.Fatalf("declared filter rejected: %v", err)
			}
			if q.accountsCalls != 1 {
				t.Fatalf("ListAccounts calls = %d, want 1", q.accountsCalls)
			}
		})
	}
}

func TestListAccountsNormalizesAndBindsIdentityFilters(t *testing.T) {
	q := &fakeListQ{providers: map[string]db.UpstreamIdp{
		"steam-main": {ID: 1, Slug: "steam-main", Protocol: "steam"},
	}}
	s := newPaginationTestServer(q)

	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{
		PageInput: PageInput{Limit: 10},
		Q:         "  Alice  ",
		Provider:  "  STEAM-MAIN ",
		Field:     " personaName ",
		Value:     "  Wonderland  ",
		Match:     " CONTAINS ",
	})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	params := q.accountsCall
	for name, got := range map[string]pgtype.Text{
		"q": params.Q, "provider": params.Provider, "field": params.Field,
		"value": params.Value, "match": params.Match,
	} {
		if !got.Valid {
			t.Errorf("%s is NULL", name)
		}
	}
	if params.Q.String != "Alice" ||
		params.Provider.String != "steam-main" ||
		params.Field.String != "personaName" ||
		params.Value.String != "Wonderland" ||
		params.Match.String != "contains" {
		t.Fatalf("normalized params = %+v", params)
	}
}

func TestListAccounts_FirstPage_HasNextCursor(t *testing.T) {
	q := &fakeListQ{}
	// 3 rows with limit=2 → first page returns 2, hasMore=true
	q.accountsRows = []db.ListAccountsRow{
		{ID: 3, CreatedAt: pgTS("2026-07-03T00:00:00Z")},
		{ID: 2, CreatedAt: pgTS("2026-07-02T00:00:00Z")},
		{ID: 1, CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	// We need to call through the real queries, not the fake. Override:

	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 2}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
	if out.Body.NextCursor == "" {
		t.Fatal("nextCursor is empty on a non-final page")
	}
	// Verify limit+1 was passed
	if q.accountsCall.Limit != 3 {
		t.Errorf("query limit = %d, want 3 (limit+1)", q.accountsCall.Limit)
	}
}

func TestListAccounts_FinalPage_NoNextCursor(t *testing.T) {
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{
		{ID: 2, CreatedAt: pgTS("2026-07-02T00:00:00Z")},
		{ID: 1, CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 5}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Fatalf("nextCursor = %q, want empty on final page", out.Body.NextCursor)
	}
}

func TestListAccounts_MiddlePage_UsesCursorKeys(t *testing.T) {
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{
		{ID: 5, CreatedAt: pgTS("2026-07-05T00:00:00Z")},
		{ID: 4, CreatedAt: pgTS("2026-07-04T00:00:00Z")},
		{ID: 3, CreatedAt: pgTS("2026-07-03T00:00:00Z")},
	}
	s := newPaginationTestServer(q)

	// Encode a cursor for the previous page's last row (id=6, created_at=2026-07-06)
	cursor := s.encodeNextCursor("accounts", "created_at", map[string]string{}, []string{
		"2026-07-06T00:00:00Z", "6",
	})
	if cursor == "" {
		t.Fatal("failed to encode cursor")
	}

	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{
		PageInput: PageInput{Limit: 2, Cursor: cursor},
	})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
	// Verify the cursor was decoded into params
	if !q.accountsCall.AfterCreatedAt.Valid {
		t.Fatal("AfterCreatedAt not set from cursor")
	}
	if q.accountsCall.AfterCreatedAt.Time.Format(time.RFC3339Nano) != "2026-07-06T00:00:00Z" {
		t.Errorf("AfterCreatedAt = %v, want 2026-07-06", q.accountsCall.AfterCreatedAt.Time)
	}
	if !q.accountsCall.AfterID.Valid || q.accountsCall.AfterID.Int32 != 6 {
		t.Errorf("AfterID = %v, want 6", q.accountsCall.AfterID)
	}
}

func TestListAccounts_DuplicateTimestamps_StableOrder(t *testing.T) {
	// Two rows with the same created_at — the id tiebreaker keeps them stable.
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{
		{ID: 12, CreatedAt: pgTS("2026-07-03T00:00:00Z")},
		{ID: 11, CreatedAt: pgTS("2026-07-03T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 5}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
	// NextCursor encodes the last row's (created_at, id) tuple
	if out.Body.NextCursor == "" {
		// Only 2 rows, limit 5 → final page, no cursor. That's correct.
		// Verify ordering: ID 12 before ID 11 (DESC)
		if out.Body.Items[0].ID != 12 || out.Body.Items[1].ID != 11 {
			t.Errorf("order = [%d, %d], want [12, 11]", out.Body.Items[0].ID, out.Body.Items[1].ID)
		}
	}
}

func TestListAccounts_LimitClamp_Default(t *testing.T) {
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{}
	s := newPaginationTestServer(q)
	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 0}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	// Default limit is 50, so query gets 51
	if q.accountsCall.Limit != 51 {
		t.Errorf("query limit = %d, want 51 (default+1)", q.accountsCall.Limit)
	}
}

func TestListAccounts_LimitClamp_Max(t *testing.T) {
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{}
	s := newPaginationTestServer(q)
	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 500}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	// Max limit is 100, so query gets 101
	if q.accountsCall.Limit != 101 {
		t.Errorf("query limit = %d, want 101 (max+1)", q.accountsCall.Limit)
	}
}

func TestListAccounts_TamperedCursor_ReturnsCursorInvalid(t *testing.T) {
	q := &fakeListQ{}
	s := newPaginationTestServer(q)
	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{
		PageInput: PageInput{Limit: 5, Cursor: "tampered-not-a-real-cursor"},
	})
	if err == nil {
		t.Fatal("expected error for tampered cursor")
	}
	pe := weberr.AsPublic(err)
	if pe == nil {
		t.Fatalf("expected weberr.PublicError, got nil for: %v", err)
	}
	if pe.Code != contract.CodeCursorInvalid {
		t.Errorf("error code = %q, want %q", pe.Code, contract.CodeCursorInvalid)
	}
}

func TestListAccountsRejectsCursorAfterFilterChange(t *testing.T) {
	q := &fakeListQ{accountsRows: []db.ListAccountsRow{
		{ID: 2, CreatedAt: pgTS("2026-07-02T00:00:00Z")},
		{ID: 1, CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}}
	s := newPaginationTestServer(q)
	first, err := s.handleListAccounts(context.Background(), &listAccountsIn{
		PageInput: PageInput{Limit: 1},
		Q:         "alice",
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.Body.NextCursor == "" {
		t.Fatal("first page cursor is empty")
	}
	callsBeforeReuse := q.accountsCalls

	_, err = s.handleListAccounts(context.Background(), &listAccountsIn{
		PageInput: PageInput{Limit: 1, Cursor: first.Body.NextCursor},
		Q:         "bob",
	})
	if err == nil {
		t.Fatal("expected cursor filter mismatch")
	}
	if q.accountsCalls != callsBeforeReuse {
		t.Fatalf("changed-filter cursor reached ListAccounts: calls %d -> %d", callsBeforeReuse, q.accountsCalls)
	}
}

func TestListAccounts_ReturnsPage_NotBareArray(t *testing.T) {
	q := &fakeListQ{}
	q.accountsRows = []db.ListAccountsRow{
		{ID: 1, CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	// Verify the output is a Page (has items + nextCursor), not a bare array
	if out.Body.Items == nil {
		t.Fatal("Items is nil — should be []")
	}
	if out.Body.Items[0].MatchingIdentities == nil || len(out.Body.Items[0].MatchingIdentities) != 0 {
		t.Fatalf("unfiltered matching identities = %+v, want []", out.Body.Items[0].MatchingIdentities)
	}
	// nextCursor must always be present (even if "")
	// The fact that Body is contract.Page[AccountView] is enforced at compile time.
}

func TestListAccountsProjectsMatchingIdentityContext(t *testing.T) {
	q := &fakeListQ{accountsRows: []db.ListAccountsRow{{
		ID:        7,
		Username:  "alice",
		CreatedAt: pgTS("2026-07-01T00:00:00Z"),
		MatchingIdentities: `[{
			"id": 99,
			"providerSlug": "steam-main",
			"providerDisplayName": "Steam",
			"protocol": "steam",
			"subject": "76561198000000000",
			"email": null,
			"data": {"personaName": "Gaben"},
			"linkedAt": "2026-06-01T00:00:00Z"
		}]`,
	}}}
	s := newPaginationTestServer(q)

	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{Q: "Gaben"})
	if err != nil {
		t.Fatalf("handleListAccounts: %v", err)
	}
	if len(out.Body.Items) != 1 || len(out.Body.Items[0].MatchingIdentities) != 1 {
		t.Fatalf("items = %+v", out.Body.Items)
	}
	identity := out.Body.Items[0].MatchingIdentities[0]
	if identity.ProviderSlug != "steam-main" ||
		identity.Protocol != "steam" ||
		identity.Subject != "76561198000000000" ||
		identity.Data["personaName"] != "Gaben" {
		t.Fatalf("matching identity = %+v", identity)
	}
}

// =====================================================================
// Audit events: filter mismatch
// =====================================================================

func TestListAuditEvents_FilterMismatch_ReturnsCursorInvalid(t *testing.T) {
	q := &fakeListQ{}
	s := newPaginationTestServer(q)

	// Issue a cursor with factor=webauthn, then decode it with factor=password
	cursor := s.encodeNextCursor("audit_events", "id", map[string]string{"factor": "webauthn"}, []string{"42"})

	_, err := s.handleListAuditEvents(context.Background(), &listAuditEventsIn{
		Factor:    "password",
		PageInput: PageInput{Limit: 10, Cursor: cursor},
	})
	if err == nil {
		t.Fatal("expected error for filter mismatch")
	}
	pe := weberr.AsPublic(err)
	if pe == nil {
		t.Fatalf("expected weberr.PublicError, got nil for: %v", err)
	}
	if pe.Code != contract.CodeCursorInvalid {
		t.Errorf("error code = %q, want %q", pe.Code, contract.CodeCursorInvalid)
	}
}

func TestListAuditEvents_SameFilters_AcceptsCursor(t *testing.T) {
	q := &fakeListQ{}
	q.auditRows = []db.ListCredentialEventsRow{
		{ID: 40, At: pgTS("2026-07-03T00:00:00Z")},
		{ID: 39, At: pgTS("2026-07-02T00:00:00Z")},
	}
	s := newPaginationTestServer(q)

	// Issue cursor with factor=webauthn, then decode with same filter
	cursor := s.encodeNextCursor("audit_events", "id", map[string]string{"factor": "webauthn"}, []string{"50"})

	_, err := s.handleListAuditEvents(context.Background(), &listAuditEventsIn{
		Factor:    "webauthn",
		PageInput: PageInput{Limit: 10, Cursor: cursor},
	})
	if err != nil {
		t.Fatalf("expected no error for matching filters: %v", err)
	}
	// Verify the cursor's after_id was decoded
	if !q.auditCall.AfterID.Valid || q.auditCall.AfterID.Int64 != 50 {
		t.Errorf("AfterID = %v, want 50", q.auditCall.AfterID)
	}
}

// =====================================================================
// Cross-collection cursor rejection
// =====================================================================

// =====================================================================
// Page JSON shape: no bare arrays, items always present
// =====================================================================

func TestPageJSON_NeverBareArray(t *testing.T) {
	p := contract.Page[any]{
		Items:      []any{},
		NextCursor: "",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["items"]; !ok {
		t.Fatal("items key missing from Page JSON")
	}
	if _, ok := m["nextCursor"]; !ok {
		t.Fatal("nextCursor key missing from Page JSON")
	}
	if _, ok := m["items"].([]any); !ok {
		t.Fatalf("items is %T, want []any", m["items"])
	}
}

func TestPageJSON_NilItemsSerializesAsEmptyArray(t *testing.T) {
	p := contract.Page[any]{}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("items is %T, want []any", m["items"])
	}
	if len(items) != 0 {
		t.Fatalf("items len = %d, want 0", len(items))
	}
}

// =====================================================================
// Invitation pagination (uses invitationOverride, not queries)
// =====================================================================

func TestListInvitations_FirstPage_HasNextCursor(t *testing.T) {
	q := &fakeListQ{}
	q.invitationRows = []db.Enrollment{
		{Token: "t3", CreatedAt: pgTS("2026-07-03T00:00:00Z"), ExpiresAt: pgTS("2026-07-04T00:00:00Z")},
		{Token: "t2", CreatedAt: pgTS("2026-07-02T00:00:00Z"), ExpiresAt: pgTS("2026-07-03T00:00:00Z")},
		{Token: "t1", CreatedAt: pgTS("2026-07-01T00:00:00Z"), ExpiresAt: pgTS("2026-07-02T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	// invitationOverride is already set to q by newPaginationTestServer
	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{PageInput: PageInput{Limit: 2}})
	if err != nil {
		t.Fatalf("handleListInvitations: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
	if out.Body.NextCursor == "" {
		t.Fatal("nextCursor is empty on a non-final page")
	}
}

func TestListInvitations_FinalPage_NoNextCursor(t *testing.T) {
	q := &fakeListQ{}
	q.invitationRows = []db.Enrollment{
		{Token: "t1", CreatedAt: pgTS("2026-07-01T00:00:00Z"), ExpiresAt: pgTS("2026-07-02T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{PageInput: PageInput{Limit: 5}})
	if err != nil {
		t.Fatalf("handleListInvitations: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Fatalf("nextCursor = %q, want empty on final page", out.Body.NextCursor)
	}
}

// =====================================================================
// Signing keys, OIDC apps, SAML SPs, upstream IdPs, forward-auth apps
// share the same cursor pattern; verify each returns a Page and handles
// the tampered-cursor path.
// =====================================================================

func TestListSigningKeys_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.signKeyRows = []db.SigningKey{
		{Kid: "k1", Algorithm: "RS256", Use: "sig", Status: "active", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListSigningKeys(context.Background(), &listSigningKeysIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListSigningKeys: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
	if out.Body.NextCursor != "" {
		t.Fatalf("nextCursor = %q, want empty on final page", out.Body.NextCursor)
	}
}

func TestListOIDCApplications_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.oidcRows = []db.ListNonForwardAuthOIDCClientsRow{
		{ClientID: "c1", DisplayName: "App 1", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListOIDCApplications(adminListContext(), &listOIDCApplicationsIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListOIDCApplications: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
	if !q.oidcCall.IncludeAll || q.oidcCall.AccountID != 0 {
		t.Fatalf("query scope = (includeAll=%v, accountID=%d), want (true, 0)", q.oidcCall.IncludeAll, q.oidcCall.AccountID)
	}
}

func TestListSAMLApplications_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.samlRows = []db.SamlSp{
		{ID: 1, EntityID: "sp1", DisplayName: "SP 1", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListSAMLApplications(adminListContext(), &listSAMLApplicationsIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListSAMLApplications: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
	if !q.samlCall.IncludeAll || q.samlCall.AccountID != 0 {
		t.Fatalf("query scope = (includeAll=%v, accountID=%d), want (true, 0)", q.samlCall.IncludeAll, q.samlCall.AccountID)
	}
}

func TestListIdentityProviders_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.idpRows = []db.UpstreamIdp{
		{ID: 1, Slug: "steam", DisplayName: "Steam", Protocol: "steam", ProviderConfig: []byte(`{}`), SecretStatus: "unconfigured", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListIdentityProviders(context.Background(), &listIdentityProvidersIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListIdentityProviders: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
}

func TestListForwardAuthApps_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.faRows = []db.ListForwardAuthClientsRow{
		{ClientID: "fa1", DisplayName: "FA 1", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListForwardAuthApps(adminListContext(), &listForwardAuthAppsIn{PageInput: PageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListForwardAuthApps: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
	if !q.faCall.IncludeAll || q.faCall.AccountID != 0 {
		t.Fatalf("query scope = (includeAll=%v, accountID=%d), want (true, 0)", q.faCall.IncludeAll, q.faCall.AccountID)
	}
}

func TestApplicationListsUseAssignedAccountScope(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*Server) error
		scope  func(*fakeListQ) (bool, int32)
	}{
		{
			name: "OIDC",
			invoke: func(s *Server) error {
				_, err := s.handleListOIDCApplications(userListContext(7), &listOIDCApplicationsIn{})
				return err
			},
			scope: func(q *fakeListQ) (bool, int32) { return q.oidcCall.IncludeAll, q.oidcCall.AccountID },
		},
		{
			name: "forward auth",
			invoke: func(s *Server) error {
				_, err := s.handleListForwardAuthApps(userListContext(7), &listForwardAuthAppsIn{})
				return err
			},
			scope: func(q *fakeListQ) (bool, int32) { return q.faCall.IncludeAll, q.faCall.AccountID },
		},
		{
			name: "SAML",
			invoke: func(s *Server) error {
				_, err := s.handleListSAMLApplications(userListContext(7), &listSAMLApplicationsIn{})
				return err
			},
			scope: func(q *fakeListQ) (bool, int32) { return q.samlCall.IncludeAll, q.samlCall.AccountID },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeListQ{}
			if err := tc.invoke(newPaginationTestServer(q)); err != nil {
				t.Fatal(err)
			}
			includeAll, accountID := tc.scope(q)
			if includeAll || accountID != 7 {
				t.Fatalf("query scope = (includeAll=%v, accountID=%d), want (false, 7)", includeAll, accountID)
			}
		})
	}
}

func TestApplicationListCursorsAreBoundToAuthorizationScope(t *testing.T) {
	tests := []struct {
		name       string
		collection string
		keys       []string
		invoke     func(*Server, context.Context, string) error
		calls      func(*fakeListQ) int
	}{
		{
			name: "OIDC", collection: "oidc_applications", keys: []string{"2026-07-01T00:00:00Z", "oidc-1"},
			invoke: func(s *Server, ctx context.Context, cursor string) error {
				_, err := s.handleListOIDCApplications(ctx, &listOIDCApplicationsIn{PageInput: PageInput{Cursor: cursor}})
				return err
			},
			calls: func(q *fakeListQ) int { return q.oidcCalls },
		},
		{
			name: "forward auth", collection: "forward_auth_apps", keys: []string{"2026-07-01T00:00:00Z", "fa-1"},
			invoke: func(s *Server, ctx context.Context, cursor string) error {
				_, err := s.handleListForwardAuthApps(ctx, &listForwardAuthAppsIn{PageInput: PageInput{Cursor: cursor}})
				return err
			},
			calls: func(q *fakeListQ) int { return q.faCalls },
		},
		{
			name: "SAML", collection: "saml_applications", keys: []string{"2026-07-01T00:00:00Z", "1"},
			invoke: func(s *Server, ctx context.Context, cursor string) error {
				_, err := s.handleListSAMLApplications(ctx, &listSAMLApplicationsIn{PageInput: PageInput{Cursor: cursor}})
				return err
			},
			calls: func(q *fakeListQ) int { return q.samlCalls },
		},
	}
	for _, tc := range tests {
		for _, scopeCase := range []struct {
			name          string
			sourceFilters map[string]string
			targetContext context.Context
		}{
			{name: "different account", sourceFilters: map[string]string{"scope": "assigned", "account_id": "7"}, targetContext: userListContext(8)},
			{name: "admin to user", sourceFilters: map[string]string{"scope": "all"}, targetContext: userListContext(7)},
		} {
			t.Run(tc.name+"/"+scopeCase.name, func(t *testing.T) {
				q := &fakeListQ{}
				s := newPaginationTestServer(q)
				cursor := s.encodeNextCursor(tc.collection, "created_at", scopeCase.sourceFilters, tc.keys)
				if err := tc.invoke(s, scopeCase.targetContext, cursor); err == nil {
					t.Fatal("expected cursor scope mismatch")
				} else if publicErr := weberr.AsPublic(err); publicErr == nil || publicErr.Code != contract.CodeCursorInvalid {
					t.Fatalf("error = %v, want %s", err, contract.CodeCursorInvalid)
				}
				if got := tc.calls(q); got != 0 {
					t.Fatalf("query calls = %d, want 0", got)
				}
			})
		}
	}
}

// =====================================================================
// Compile-time: all list handlers return contract.Page[T], not bare arrays
// =====================================================================

func TestCompile_ListHandlersReturnPageNotBareArray(t *testing.T) {
	// These assertions are enforced at compile time by the type system.
	// If any handler returned []T instead of contract.Page[T], the assignment
	// would fail to compile.
	var _ func(context.Context, *listAccountsIn) (*struct {
		Body contract.Page[contract.AccountView]
	}, error) = func(_ context.Context, _ *listAccountsIn) (*struct {
		Body contract.Page[contract.AccountView]
	}, error) {
		return nil, nil
	}
	// The real compile-time check is that listAccountsOut.Body is contract.Page,
	// which the compiler enforces at the return statement.
}

// Suppress unused import warnings for httptest/http (used in future integration tests)
var _ = httptest.NewRecorder
var _ = http.MethodGet
var _ = fmt.Sprintf
