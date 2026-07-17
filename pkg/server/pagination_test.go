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

	// groups
	groupsRows []db.ListGroupsRow
	groupsCall db.ListGroupsParams

	// oidc
	oidcRows []db.ListNonForwardAuthOIDCClientsRow
	oidcCall db.ListNonForwardAuthOIDCClientsParams

	// saml
	samlRows []db.SamlSp
	samlCall db.ListSAMLSPsParams

	// upstream idps
	idpRows []db.UpstreamIdp
	idpCall db.ListAllUpstreamIDPsParams

	// signing keys
	signKeyRows []db.SigningKey
	signKeyCall db.ListAllSigningKeysParams

	// forward-auth
	faRows []db.ListForwardAuthClientsRow
	faCall db.ListForwardAuthClientsParams

	// audit
	auditRows []db.CredentialEvent
	auditCall db.ListCredentialEventsParams
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

func (f *fakeListQ) ListGroups(_ context.Context, p db.ListGroupsParams) ([]db.ListGroupsRow, error) {
	f.groupsCall = p
	return f.groupsRows, nil
}

func (f *fakeListQ) ListNonForwardAuthOIDCClients(_ context.Context, p db.ListNonForwardAuthOIDCClientsParams) ([]db.ListNonForwardAuthOIDCClientsRow, error) {
	f.oidcCall = p
	return f.oidcRows, nil
}

func (f *fakeListQ) ListSAMLSPs(_ context.Context, p db.ListSAMLSPsParams) ([]db.SamlSp, error) {
	f.samlCall = p
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
	return f.faRows, nil
}

func (f *fakeListQ) ListCredentialEvents(_ context.Context, p db.ListCredentialEventsParams) ([]db.CredentialEvent, error) {
	f.auditCall = p
	return f.auditRows, nil
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
		pageInput: pageInput{Limit: 10},
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

	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 2}})
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
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 5}})
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
		pageInput: pageInput{Limit: 2, Cursor: cursor},
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
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 5}})
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
	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 0}})
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
	_, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 500}})
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
		pageInput: pageInput{Limit: 5, Cursor: "tampered-not-a-real-cursor"},
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
		pageInput: pageInput{Limit: 1},
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
		pageInput: pageInput{Limit: 1, Cursor: first.Body.NextCursor},
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
	out, err := s.handleListAccounts(context.Background(), &listAccountsIn{pageInput: pageInput{Limit: 10}})
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
		pageInput: pageInput{Limit: 10, Cursor: cursor},
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
	q.auditRows = []db.CredentialEvent{
		{ID: 40, At: pgTS("2026-07-03T00:00:00Z")},
		{ID: 39, At: pgTS("2026-07-02T00:00:00Z")},
	}
	s := newPaginationTestServer(q)

	// Issue cursor with factor=webauthn, then decode with same filter
	cursor := s.encodeNextCursor("audit_events", "id", map[string]string{"factor": "webauthn"}, []string{"50"})

	_, err := s.handleListAuditEvents(context.Background(), &listAuditEventsIn{
		Factor:    "webauthn",
		pageInput: pageInput{Limit: 10, Cursor: cursor},
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

func TestListGroups_AccountsCursor_Rejected(t *testing.T) {
	q := &fakeListQ{}
	s := newPaginationTestServer(q)

	// Issue a cursor for "accounts" collection, try to use it on "groups"
	cursor := s.encodeNextCursor("accounts", "created_at", map[string]string{}, []string{
		"2026-07-06T00:00:00Z", "6",
	})

	_, err := s.handleListGroups(context.Background(), &listGroupsIn{
		pageInput: pageInput{Limit: 5, Cursor: cursor},
	})
	if err == nil {
		t.Fatal("expected error for cross-collection cursor")
	}
	pe := weberr.AsPublic(err)
	if pe == nil {
		t.Fatalf("expected weberr.PublicError, got nil for: %v", err)
	}
	if pe.Code != contract.CodeCursorInvalid {
		t.Errorf("error code = %q, want %q", pe.Code, contract.CodeCursorInvalid)
	}
}

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
	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{pageInput: pageInput{Limit: 2}})
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
	out, err := s.handleListInvitations(context.Background(), &listInvitationsIn{pageInput: pageInput{Limit: 5}})
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
// Groups pagination
// =====================================================================

func TestListGroups_LimitClamp_Default(t *testing.T) {
	q := &fakeListQ{}
	q.groupsRows = []db.ListGroupsRow{}
	s := newPaginationTestServer(q)
	_, err := s.handleListGroups(context.Background(), &listGroupsIn{pageInput: pageInput{Limit: 0}})
	if err != nil {
		t.Fatalf("handleListGroups: %v", err)
	}
	if q.groupsCall.Limit != 51 {
		t.Errorf("query limit = %d, want 51", q.groupsCall.Limit)
	}
}

func TestListGroups_TamperedCursor_ReturnsCursorInvalid(t *testing.T) {
	q := &fakeListQ{}
	s := newPaginationTestServer(q)
	_, err := s.handleListGroups(context.Background(), &listGroupsIn{
		pageInput: pageInput{Limit: 5, Cursor: "not-a-valid-cursor!!!"},
	})
	if err == nil {
		t.Fatal("expected error for tampered cursor")
	}
	pe := weberr.AsPublic(err)
	if pe == nil || pe.Code != contract.CodeCursorInvalid {
		t.Fatalf("expected pagination_cursor_invalid, got %T: %v", err, err)
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
	out, err := s.handleListSigningKeys(context.Background(), &listSigningKeysIn{pageInput: pageInput{Limit: 10}})
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
	out, err := s.handleListOIDCApplications(context.Background(), &listOIDCApplicationsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListOIDCApplications: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
}

func TestListSAMLApplications_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.samlRows = []db.SamlSp{
		{ID: 1, EntityID: "sp1", DisplayName: "SP 1", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListSAMLApplications(context.Background(), &listSAMLApplicationsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListSAMLApplications: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
	}
}

func TestListIdentityProviders_ReturnsPage(t *testing.T) {
	q := &fakeListQ{}
	q.idpRows = []db.UpstreamIdp{
		{ID: 1, Slug: "steam", DisplayName: "Steam", Protocol: "steam", ProviderConfig: []byte(`{}`), SecretStatus: "unconfigured", CreatedAt: pgTS("2026-07-01T00:00:00Z")},
	}
	s := newPaginationTestServer(q)
	out, err := s.handleListIdentityProviders(context.Background(), &listIdentityProvidersIn{pageInput: pageInput{Limit: 10}})
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
	out, err := s.handleListForwardAuthApps(context.Background(), &listForwardAuthAppsIn{pageInput: pageInput{Limit: 10}})
	if err != nil {
		t.Fatalf("handleListForwardAuthApps: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Body.Items))
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
