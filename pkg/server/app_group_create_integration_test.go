package server

// Package server — app_group_create_integration_test.go
//
// PHB-134 regression coverage at the database level. The
// managed-application group-create endpoint used to answer 404
// group_not_found even though both rows were written, because
// CreateOIDCAppGroup and CreateSAMLAppGroup ended with a SELECT that joined
// the base user_group table — invisible to the statement's own snapshot
// after the CTE's INSERT — instead of reading the CTE's RETURNING set. The
// unit suite never saw this: its in-memory stub returned the group by
// construction. These tests run the real handlers against real Postgres and
// assert the 201, the response body, and the stored rows together.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// createAppGroupResponseBody asserts the 201 and the response body fields the
// bug emptied, and returns the created view for row-level checks.
func createAppGroupResponseBody(t *testing.T, rr *httptest.ResponseRecorder) contract.AppGroupView {
	t.Helper()
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rr.Code, rr.Body.String())
	}
	var view contract.AppGroupView
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rr.Body.String())
	}
	if view.ID == 0 || view.Slug == "" || view.DisplayName == "" || !view.ExposedToDownstream {
		t.Fatalf("response carries no created group: %#v", view)
	}
	return view
}

// assertStoredAppGroup reads user_group and the app's link table through a
// fresh connection-level query, proving the writes actually committed.
func assertStoredAppGroup(t *testing.T, base *writeFixFixture, groupID int32, kind, slug, displayName string) {
	t.Helper()
	ctx := context.Background()
	var storedKind, storedSlug, storedName string
	if err := base.pool.QueryRow(ctx, `
		SELECT kind, slug, display_name FROM user_group WHERE id = $1`, groupID,
	).Scan(&storedKind, &storedSlug, &storedName); err != nil {
		t.Fatalf("read stored group %d: %v — rows were written but the response came back empty", groupID, err)
	}
	if storedKind != kind || storedSlug != slug || storedName != displayName {
		t.Errorf("stored row = (%s, %s, %s), want (%s, %s, %s)", storedKind, storedSlug, storedName, kind, slug, displayName)
	}
}

// TestCreateManagedApplicationGroupReturns201AndRowsPostgres drives the admin
// create-app-group handler for an OIDC and a SAML application against a live
// database. The PHB-134 bug class — writes committed, answer 404 — must fail
// this test at either the status check or the row check.
func TestCreateManagedApplicationGroupReturns201AndRowsPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)

	clientID := "group-create-" + suffix
	if _, err := base.pool.Exec(ctx, `
		INSERT INTO oidc_client (client_id, display_name, redirect_uris, allowed_scopes)
		VALUES ($1, 'Group Create', ARRAY['https://example.test/callback'], ARRAY['openid'])`, clientID); err != nil {
		t.Fatal(err)
	}
	var samlSPID int64
	if err := base.pool.QueryRow(ctx, `
		INSERT INTO saml_sp (entity_id, display_name)
		VALUES ($1, 'Group Create SAML') RETURNING id`, "https://example.test/group-create/"+suffix).Scan(&samlSPID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM oidc_client WHERE client_id = $1`, clientID)
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM saml_sp WHERE id = $1`, samlSPID)
	})

	s := writeFixServer(base)

	t.Run("OIDC", func(t *testing.T) {
		body := `{"kind":"manual","slug":"oidc-created-` + suffix + `","displayName":"OIDC Created","description":"via endpoint"}`
		req := reqWithSession(http.MethodPost, "/api/prohibitorum/managed-applications/oidc/"+clientID+"/groups", body, "", base.server.sess)
		req = withRouteParams(req, "kind", "oidc", "appId", clientID)
		rr := httptest.NewRecorder()
		s.handleCreateManagedApplicationGroupHTTP(rr, req)

		view := createAppGroupResponseBody(t, rr)
		if view.Kind != "manual" || view.Slug != "oidc-created-"+suffix || view.DisplayName != "OIDC Created" || view.Description != "via endpoint" {
			t.Fatalf("response body = %#v", view)
		}
		assertStoredAppGroup(t, base, view.ID, "manual", "oidc-created-"+suffix, "OIDC Created")
		var linked int
		if err := base.pool.QueryRow(ctx, `
			SELECT count(*) FROM oidc_client_group WHERE client_id = $1 AND group_id = $2`, clientID, view.ID).Scan(&linked); err != nil {
			t.Fatal(err)
		}
		if linked != 1 {
			t.Errorf("oidc_client_group links = %d, want 1", linked)
		}
	})

	t.Run("SAML", func(t *testing.T) {
		body := `{"kind":"manual","slug":"saml-created-` + suffix + `","displayName":"SAML Created"}`
		req := reqWithSession(http.MethodPost,
			"/api/prohibitorum/managed-applications/saml/"+strconvI64(samlSPID)+"/groups", body, "", base.server.sess)
		req = withRouteParams(req, "kind", "saml", "appId", strconvI64(samlSPID))
		rr := httptest.NewRecorder()
		s.handleCreateManagedApplicationGroupHTTP(rr, req)

		view := createAppGroupResponseBody(t, rr)
		if view.Kind != "manual" || view.Slug != "saml-created-"+suffix || view.DisplayName != "SAML Created" {
			t.Fatalf("response body = %#v", view)
		}
		assertStoredAppGroup(t, base, view.ID, "manual", "saml-created-"+suffix, "SAML Created")
		var linked int
		if err := base.pool.QueryRow(ctx, `
			SELECT count(*) FROM saml_sp_group WHERE saml_sp_id = $1 AND group_id = $2`, samlSPID, view.ID).Scan(&linked); err != nil {
			t.Fatal(err)
		}
		if linked != 1 {
			t.Errorf("saml_sp_group links = %d, want 1", linked)
		}
	})
}

// strconvI64 keeps the test body readable where the SAML app id is a path
// segment.
func strconvI64(v int64) string {
	return strconv.FormatInt(v, 10)
}

// withRouteParams injects several chi URL parameters in one route context,
// the way the real router does.
func withRouteParams(req *http.Request, pairs ...string) *http.Request {
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(pairs); i += 2 {
		rctx.URLParams.Add(pairs[i], pairs[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestCreateSAMLAppGroupReturnsCreatedRowPostgres pins the sqlc query itself:
// the statement must answer the just-inserted row from the CTE's RETURNING
// set, and both the group and its saml_sp_group link must be visible to a
// later read. Reverting the query to SELECT from the base table fails here
// with pgx.ErrNoRows.
func TestCreateSAMLAppGroupReturnsCreatedRowPostgres(t *testing.T) {
	base := newWriteFixFixture(t)
	ctx := context.Background()
	suffix := writeFixSuffix(t)

	var samlSPID int64
	if err := base.pool.QueryRow(ctx, `
		INSERT INTO saml_sp (entity_id, display_name)
		VALUES ($1, 'Query SAML') RETURNING id`, "https://example.test/query-saml/"+suffix).Scan(&samlSPID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.pool.Exec(context.Background(), `DELETE FROM saml_sp WHERE id = $1`, samlSPID)
	})

	q := db.New(base.pool)
	created, err := q.CreateSAMLAppGroup(ctx, db.CreateSAMLAppGroupParams{
		Kind:                "manual",
		Slug:                "query-saml-" + suffix,
		DisplayName:         "Query SAML Group",
		Description:         pgtype.Text{String: "direct query", Valid: true},
		ExposedToDownstream: true,
		SamlSpID:            samlSPID,
	})
	if err != nil {
		t.Fatalf("CreateSAMLAppGroup: %v — the query returned no row for the group it just inserted", err)
	}
	if created.ID == 0 || created.Kind != "manual" || created.Slug != "query-saml-"+suffix ||
		created.DisplayName != "Query SAML Group" || !created.Description.Valid || created.Description.String != "direct query" ||
		!created.ExposedToDownstream || !created.CreatedAt.Valid || !created.UpdatedAt.Valid {
		t.Fatalf("returned row = %#v", created)
	}

	assertStoredAppGroup(t, base, created.ID, "manual", "query-saml-"+suffix, "Query SAML Group")
	var linked int
	if err := base.pool.QueryRow(ctx, `
		SELECT count(*) FROM saml_sp_group WHERE saml_sp_id = $1 AND group_id = $2`, samlSPID, created.ID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 1 {
		t.Errorf("saml_sp_group links = %d, want 1", linked)
	}
}
