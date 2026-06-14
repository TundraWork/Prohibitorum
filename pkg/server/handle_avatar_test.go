// Package server — handle_avatar_test.go
//
// Unit tests for the dual-source avatar handlers:
//   PUT  /api/prohibitorum/me/avatar           — upload → user source + active=user
//   PUT  /api/prohibitorum/me/avatar/selection — change active source
//   DELETE /api/prohibitorum/me/avatar         — delete user upload + fallback
//   GET  /avatar/{subject}                     — public; active or ?source= specific
//
// All tests use a fake avatarQueries stub (no real DB). The production
// transaction path is exercised by smoke tests; unit tests focus on handler
// logic, error codes, ETag conditional-GET, and the nil-dbPool seam.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	_ "github.com/gen2brain/webp" // register webp decoder for image.DecodeConfig

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
)

// ---------------------------------------------------------------------------
// Fake avatarQueries — new dual-source interface
// ---------------------------------------------------------------------------

type sourceKey struct {
	accountID int32
	source    string
}

type avatarRow struct {
	bytes       []byte
	etag        pgtype.Text
	contentType pgtype.Text
}

type fakeAvatarQueries struct {
	// in-memory avatar store: keyed by (accountID, source)
	store map[sourceKey]avatarRow
	// per-account active source pointer (mirrors account.avatar_source)
	activeSource map[int32]string // "" means never set / NULL
	// subject → accountID for GET queries
	subjectMap map[string]int32
	disabled   map[int32]bool
}

func newFakeAvatarQ() *fakeAvatarQueries {
	return &fakeAvatarQueries{
		store:        make(map[sourceKey]avatarRow),
		activeSource: make(map[int32]string),
		subjectMap:   make(map[string]int32),
		disabled:     make(map[int32]bool),
	}
}

func (f *fakeAvatarQueries) UpsertAvatarSource(_ context.Context, arg db.UpsertAvatarSourceParams) error {
	f.store[sourceKey{arg.AccountID, arg.Source}] = avatarRow{
		bytes:       arg.Bytes,
		etag:        arg.Etag,
		contentType: arg.ContentType,
	}
	return nil
}

func (f *fakeAvatarQueries) SetActiveAvatar(_ context.Context, arg db.SetActiveAvatarParams) error {
	f.activeSource[arg.AccountID] = arg.Source
	return nil
}

func (f *fakeAvatarQueries) ClearActiveAvatar(_ context.Context, arg db.ClearActiveAvatarParams) error {
	f.activeSource[arg.AccountID] = arg.Source // stores "none"
	return nil
}

func (f *fakeAvatarQueries) DeleteAvatarSource(_ context.Context, arg db.DeleteAvatarSourceParams) error {
	delete(f.store, sourceKey{arg.AccountID, arg.Source})
	return nil
}

func (f *fakeAvatarQueries) GetActiveAvatarBySubject(_ context.Context, sub pgtype.UUID) (db.GetActiveAvatarBySubjectRow, error) {
	subStr := sub.String()
	acctID, ok := f.subjectMap[subStr]
	if !ok {
		return db.GetActiveAvatarBySubjectRow{}, errNoRows
	}
	active := f.activeSource[acctID]
	if active == "" || active == "none" {
		return db.GetActiveAvatarBySubjectRow{}, errNoRows
	}
	row, ok := f.store[sourceKey{acctID, active}]
	if !ok {
		return db.GetActiveAvatarBySubjectRow{}, errNoRows
	}
	return db.GetActiveAvatarBySubjectRow{
		Bytes:       row.bytes,
		ContentType: row.contentType,
		Etag:        row.etag,
		Disabled:    f.disabled[acctID],
	}, nil
}

func (f *fakeAvatarQueries) GetAvatarSourceBySubject(_ context.Context, arg db.GetAvatarSourceBySubjectParams) (db.GetAvatarSourceBySubjectRow, error) {
	subStr := arg.OidcSubject.String()
	acctID, ok := f.subjectMap[subStr]
	if !ok {
		return db.GetAvatarSourceBySubjectRow{}, errNoRows
	}
	row, ok := f.store[sourceKey{acctID, arg.Source}]
	if !ok {
		return db.GetAvatarSourceBySubjectRow{}, errNoRows
	}
	return db.GetAvatarSourceBySubjectRow{
		Bytes:       row.bytes,
		ContentType: row.contentType,
		Etag:        row.etag,
		Disabled:    f.disabled[acctID],
	}, nil
}

func (f *fakeAvatarQueries) ListAvatarSourcesByAccount(_ context.Context, accountID int32) ([]db.ListAvatarSourcesByAccountRow, error) {
	var rows []db.ListAvatarSourcesByAccountRow
	for k, v := range f.store {
		if k.accountID == accountID {
			rows = append(rows, db.ListAvatarSourcesByAccountRow{
				Source: k.source,
				Etag:   v.etag,
			})
		}
	}
	return rows, nil
}

// errNoRows is used by the fake to signal "not found".
// Must be pgx.ErrNoRows so that errors.Is checks in the handlers work correctly.
var errNoRows = pgx.ErrNoRows

// Compile-time check: fakeAvatarQueries must satisfy the avatarQueries interface.
var _ avatarQueries = (*fakeAvatarQueries)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testAccountID int32 = 42
const testSubject = "11111111-2222-3333-4444-555555555555"

// smallPNG builds a minimal PNG image suitable for Process().
func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// newAvatarServer builds the minimal *Server needed for avatar handler tests.
func newAvatarServer(t *testing.T, q *fakeAvatarQueries) *Server {
	t.Helper()
	return &Server{
		avatarQueriesOverride: q,
		// dbPool left nil → handlers use the nil-dbPool seam (no tx).
	}
}

// avatarSession returns a session for testAccountID with the given OidcSubject.
func avatarSession(sub string) *authn.Session {
	var subUUID pgtype.UUID
	_ = subUUID.Scan(sub)
	return &authn.Session{
		Account: &db.Account{
			ID:          testAccountID,
			Username:    "testuser",
			DisplayName: "Test User",
			Role:        "user",
			OidcSubject: subUUID,
		},
		Data: &authn.SessionData{},
	}
}

// putAvatarReq builds a PUT request with the given body attached to a session.
func putAvatarReq(t *testing.T, body []byte, sess *authn.Session) *http.Request {
	t.Helper()
	r := httptest.NewRequest("PUT", "/api/prohibitorum/me/avatar", bytes.NewReader(body))
	if sess != nil {
		r = r.WithContext(authn.WithSession(r.Context(), sess))
	}
	return r
}

// deleteAvatarReq builds a DELETE request optionally attached to a session.
func deleteAvatarReq(t *testing.T, sess *authn.Session) *http.Request {
	t.Helper()
	r := httptest.NewRequest("DELETE", "/api/prohibitorum/me/avatar", nil)
	if sess != nil {
		r = r.WithContext(authn.WithSession(r.Context(), sess))
	}
	return r
}

// getAvatarReq builds a GET request for /avatar/{subject} with chi URLParam.
// If source is non-empty, it is added as a query parameter.
func getAvatarReq(t *testing.T, sub string, ifNoneMatch string, source string) *http.Request {
	t.Helper()
	url := "/avatar/" + sub
	if source != "" {
		url += "?source=" + source
	}
	r := httptest.NewRequest("GET", url, nil)
	if ifNoneMatch != "" {
		r.Header.Set("If-None-Match", ifNoneMatch)
	}
	// Install chi route context so chi.URLParam works without a real router.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subject", sub)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// putSelectionReq builds a PUT request for /me/avatar/selection with JSON body.
func putSelectionReq(t *testing.T, source string, sess *authn.Session) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"source": source})
	r := httptest.NewRequest("PUT", "/api/prohibitorum/me/avatar/selection", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if sess != nil {
		r = r.WithContext(authn.WithSession(r.Context(), sess))
	}
	return r
}

// decodeAvatarErrCode returns the "code" field from a JSON error body.
func decodeAvatarErrCode(t *testing.T, body string) string {
	t.Helper()
	m := decodeJSON(t, []byte(body))
	code, _ := m["code"].(string)
	return code
}

// seedUserAvatar uploads a small PNG via PUT and registers the subject mapping.
// Returns the etag.
func seedUserAvatar(t *testing.T, s *Server, q *fakeAvatarQueries, sess *authn.Session) string {
	t.Helper()
	pr := putAvatarReq(t, smallPNG(t), sess)
	pw := httptest.NewRecorder()
	s.handlePutAvatarHTTP(pw, pr)
	if pw.Code != http.StatusNoContent {
		t.Fatalf("seed PUT: want 204, got %d; body=%s", pw.Code, pw.Body.String())
	}
	q.subjectMap[testSubject] = testAccountID
	row := q.store[sourceKey{testAccountID, "user"}]
	return row.etag.String
}

// seedUpstreamAvatar directly injects an upstream row in the fake store.
func seedUpstreamAvatar(q *fakeAvatarQueries, accountID int32, etag string) {
	q.store[sourceKey{accountID, "upstream"}] = avatarRow{
		bytes:       []byte("fake-upstream-webp"),
		etag:        pgtype.Text{String: etag, Valid: true},
		contentType: pgtype.Text{String: "image/webp", Valid: true},
	}
}

// ---------------------------------------------------------------------------
// PUT /me/avatar tests
// ---------------------------------------------------------------------------

// TestPutAvatar_ValidPNG_Stores204 verifies that uploading a valid PNG produces
// a 204, stores a 'user' source row, sets active=user, and refreshes the session.
func TestPutAvatar_ValidPNG_Stores204(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	r := putAvatarReq(t, smallPNG(t), sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d; body=%s", w.Code, w.Body.String())
	}
	// User source row must exist.
	userRow, ok := q.store[sourceKey{testAccountID, "user"}]
	if !ok || len(userRow.bytes) == 0 {
		t.Error("no user avatar bytes stored")
	}
	if !userRow.etag.Valid || userRow.etag.String == "" {
		t.Error("user avatar etag must be set")
	}
	if userRow.contentType.String != "image/webp" {
		t.Errorf("content_type: want image/webp, got %q", userRow.contentType.String)
	}
	// Active source must be 'user'.
	if q.activeSource[testAccountID] != "user" {
		t.Errorf("active source: want user, got %q", q.activeSource[testAccountID])
	}
	// In-memory session must reflect new state.
	if !sess.Account.AvatarEtag.Valid || sess.Account.AvatarEtag.String == "" {
		t.Error("sess.Account.AvatarEtag must be valid after PUT")
	}
	if sess.Account.AvatarSource.String != "user" {
		t.Errorf("sess.Account.AvatarSource: want user, got %q", sess.Account.AvatarSource.String)
	}
}

// TestPutAvatar_StoresWebP512 verifies that the stored bytes decode as 512x512 WebP.
func TestPutAvatar_StoresWebP512(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	r := putAvatarReq(t, smallPNG(t), sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d", w.Code)
	}
	stored := q.store[sourceKey{testAccountID, "user"}].bytes
	cfg, format, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil {
		t.Fatalf("stored bytes not decodable: %v", err)
	}
	if format != "webp" {
		t.Errorf("format: want webp, got %q", format)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Errorf("size: want 512x512, got %dx%d", cfg.Width, cfg.Height)
	}
}

// TestPutAvatar_TooLarge_400 verifies that a 6 MiB body returns 400 avatar_too_large.
func TestPutAvatar_TooLarge_400(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	r := putAvatarReq(t, make([]byte, 6<<20), sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d; body=%s", w.Code, w.Body.String())
	}
	if code := decodeAvatarErrCode(t, w.Body.String()); code != "avatar_too_large" {
		t.Errorf("code: want avatar_too_large, got %q", code)
	}
}

// TestPutAvatar_Garbage_400 verifies that garbage bytes return 400 avatar_invalid_image.
func TestPutAvatar_Garbage_400(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	r := putAvatarReq(t, []byte("not an image at all"), sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d; body=%s", w.Code, w.Body.String())
	}
	if code := decodeAvatarErrCode(t, w.Body.String()); code != "avatar_invalid_image" {
		t.Errorf("code: want avatar_invalid_image, got %q", code)
	}
}

// TestPutAvatar_NoSession_401 verifies that PUT without a session returns 401.
func TestPutAvatar_NoSession_401(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)

	r := putAvatarReq(t, smallPNG(t), nil) // no session
	w := httptest.NewRecorder()
	s.handlePutAvatarHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d; body=%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PUT /me/avatar/selection tests
// ---------------------------------------------------------------------------

// TestPutAvatarSelection_SwitchToUpstream_204 verifies that selecting "upstream"
// when an upstream row exists returns 204 and sets active=upstream.
func TestPutAvatarSelection_SwitchToUpstream_204(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	// Seed a user upload so active starts as user.
	seedUserAvatar(t, s, q, sess)
	// Also inject an upstream row.
	seedUpstreamAvatar(q, testAccountID, "upstream-etag-abcdef")

	r := putSelectionReq(t, "upstream", sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarSelectionHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d; body=%s", w.Code, w.Body.String())
	}
	if q.activeSource[testAccountID] != "upstream" {
		t.Errorf("active source: want upstream, got %q", q.activeSource[testAccountID])
	}
	if sess.Account.AvatarSource.String != "upstream" {
		t.Errorf("sess AvatarSource: want upstream, got %q", sess.Account.AvatarSource.String)
	}
}

// TestPutAvatarSelection_UpstreamMissing_400 verifies that selecting "upstream"
// when NO upstream row exists returns 400 avatar_source_unavailable.
func TestPutAvatarSelection_UpstreamMissing_400(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	// Only user row exists — no upstream.
	seedUserAvatar(t, s, q, sess)

	r := putSelectionReq(t, "upstream", sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarSelectionHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d; body=%s", w.Code, w.Body.String())
	}
	if code := decodeAvatarErrCode(t, w.Body.String()); code != "avatar_source_unavailable" {
		t.Errorf("code: want avatar_source_unavailable, got %q", code)
	}
}

// TestPutAvatarSelection_None_204 verifies that selecting "none" sets active=none
// and clears the session etag/ct.
func TestPutAvatarSelection_None_204(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedUserAvatar(t, s, q, sess)

	r := putSelectionReq(t, "none", sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarSelectionHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d; body=%s", w.Code, w.Body.String())
	}
	if q.activeSource[testAccountID] != "none" {
		t.Errorf("active source: want none, got %q", q.activeSource[testAccountID])
	}
	if sess.Account.AvatarSource.String != "none" {
		t.Errorf("sess AvatarSource: want none, got %q", sess.Account.AvatarSource.String)
	}
	if sess.Account.AvatarEtag.Valid {
		t.Error("sess AvatarEtag must be cleared when source=none")
	}
}

// TestPutAvatarSelection_UnknownSource_400 verifies that an unknown source value
// returns 400 avatar_source_unavailable.
func TestPutAvatarSelection_UnknownSource_400(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	r := putSelectionReq(t, "gravatar", sess)
	w := httptest.NewRecorder()
	s.handlePutAvatarSelectionHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d; body=%s", w.Code, w.Body.String())
	}
	if code := decodeAvatarErrCode(t, w.Body.String()); code != "avatar_source_unavailable" {
		t.Errorf("code: want avatar_source_unavailable, got %q", code)
	}
}

// TestPutAvatarSelection_NoSession_401 verifies that the selection endpoint
// requires auth.
func TestPutAvatarSelection_NoSession_401(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)

	r := putSelectionReq(t, "none", nil)
	w := httptest.NewRecorder()
	s.handlePutAvatarSelectionHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /me/avatar tests
// ---------------------------------------------------------------------------

// TestDeleteAvatar_WithUpstreamFallback_204 verifies that when active was 'user'
// and an 'upstream' row exists, DELETE falls back to upstream.
func TestDeleteAvatar_WithUpstreamFallback_204(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedUserAvatar(t, s, q, sess)
	seedUpstreamAvatar(q, testAccountID, "upstream-etag-xyzw")

	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)

	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE status: want 204, got %d; body=%s", dw.Code, dw.Body.String())
	}
	// User row must be gone.
	if _, ok := q.store[sourceKey{testAccountID, "user"}]; ok {
		t.Error("user row must be deleted")
	}
	// Active must fall back to upstream.
	if q.activeSource[testAccountID] != "upstream" {
		t.Errorf("active source: want upstream, got %q", q.activeSource[testAccountID])
	}
	if sess.Account.AvatarSource.String != "upstream" {
		t.Errorf("sess AvatarSource: want upstream after fallback, got %q", sess.Account.AvatarSource.String)
	}
}

// TestDeleteAvatar_NoUpstreamFallback_204 verifies that when active was 'user'
// and NO upstream row exists, DELETE sets active=none.
func TestDeleteAvatar_NoUpstreamFallback_204(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedUserAvatar(t, s, q, sess)
	// No upstream row seeded.

	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)

	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE status: want 204, got %d; body=%s", dw.Code, dw.Body.String())
	}
	if _, ok := q.store[sourceKey{testAccountID, "user"}]; ok {
		t.Error("user row must be deleted")
	}
	if q.activeSource[testAccountID] != "none" {
		t.Errorf("active source: want none, got %q", q.activeSource[testAccountID])
	}
	if sess.Account.AvatarSource.String != "none" {
		t.Errorf("sess AvatarSource: want none, got %q", sess.Account.AvatarSource.String)
	}
	if sess.Account.AvatarEtag.Valid {
		t.Error("sess AvatarEtag must be cleared")
	}
}

// TestDeleteAvatar_NoSession_401 verifies that DELETE without a session returns 401.
func TestDeleteAvatar_NoSession_401(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)

	r := deleteAvatarReq(t, nil) // no session
	w := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d; body=%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /avatar/{subject} tests
// ---------------------------------------------------------------------------

// TestGetAvatar_200_ValidBytes verifies GET returns 200, Content-Type: image/webp,
// non-empty ETag, and bytes that decode as 512x512 WebP.
func TestGetAvatar_200_ValidBytes(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	etag := seedUserAvatar(t, s, q, sess)

	r := getAvatarReq(t, testSubject, "", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("Content-Type: want image/webp, got %q", ct)
	}
	if e := w.Header().Get("ETag"); e == "" {
		t.Error("ETag must be set")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "public") {
		t.Errorf("Cache-Control should contain public; got %q", w.Header().Get("Cache-Control"))
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("response body not decodable as image: %v", err)
	}
	if format != "webp" {
		t.Errorf("format: want webp, got %q", format)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Errorf("size: want 512x512, got %dx%d", cfg.Width, cfg.Height)
	}
	_ = etag
}

// TestGetAvatar_304_IfNoneMatch verifies that If-None-Match with the current
// ETag returns 304 with no body.
func TestGetAvatar_304_IfNoneMatch(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	etag := seedUserAvatar(t, s, q, sess)
	quotedEtag := `"` + etag + `"`

	r := getAvatarReq(t, testSubject, quotedEtag, "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotModified {
		t.Fatalf("status: want 304, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("304 body must be empty, got %d bytes", w.Body.Len())
	}
}

// TestGetAvatar_404_UnknownSubject verifies that a well-formed UUID with no
// avatar returns 404.
func TestGetAvatar_404_UnknownSubject(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)

	r := getAvatarReq(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestGetAvatar_404_BadUUID verifies that a non-UUID subject returns 404.
func TestGetAvatar_404_BadUUID(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)

	r := getAvatarReq(t, "not-a-uuid", "", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestGetAvatar_404_ActiveNone verifies that GET returns 404 when active is "none".
func TestGetAvatar_404_ActiveNone(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	q.subjectMap[testSubject] = testAccountID
	q.activeSource[testAccountID] = "none"

	r := getAvatarReq(t, testSubject, "", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestGetAvatar_404_DisabledAccount verifies that a disabled account's avatar
// returns 404.
func TestGetAvatar_404_DisabledAccount(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedUserAvatar(t, s, q, sess)
	q.disabled[testAccountID] = true

	r := getAvatarReq(t, testSubject, "", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestGetAvatar_SourceQuery_Upstream verifies that ?source=upstream serves the
// upstream row directly (bypassing the active-source join).
func TestGetAvatar_SourceQuery_Upstream(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	q.subjectMap[testSubject] = testAccountID
	seedUpstreamAvatar(q, testAccountID, "upstreamet")

	r := getAvatarReq(t, testSubject, "", "upstream")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != "fake-upstream-webp" {
		t.Errorf("body: want fake-upstream-webp, got %q", w.Body.String())
	}
}

// TestGetAvatar_SourceQuery_404_Missing verifies that ?source=upstream when no
// upstream row exists returns 404.
func TestGetAvatar_SourceQuery_404_Missing(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	q.subjectMap[testSubject] = testAccountID
	// No upstream row.

	r := getAvatarReq(t, testSubject, "", "upstream")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestDeleteAvatar_ThenGet_404 verifies that after DELETE (no upstream fallback),
// GET returns 404.
func TestDeleteAvatar_ThenGet_404(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedUserAvatar(t, s, q, sess)

	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", dw.Code)
	}

	gr := getAvatarReq(t, testSubject, "", "")
	gw := httptest.NewRecorder()
	s.handleGetAvatarHTTP(gw, gr)

	if gw.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", gw.Code)
	}
}

// TestDeleteAvatar_UpstreamActive_DeleteUser_StaysUpstream verifies that when
// active was "upstream" (not "user"), deleting the user upload does NOT change
// the active pointer: the active source remains "upstream" and a subsequent
// GET /avatar/{subject} still serves the upstream image (200).
func TestDeleteAvatar_UpstreamActive_DeleteUser_StaysUpstream(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	// Seed a user upload (this also sets active=user in the fake).
	seedUserAvatar(t, s, q, sess)
	// Inject an upstream row.
	seedUpstreamAvatar(q, testAccountID, "upstream-etag-sticky")
	q.subjectMap[testSubject] = testAccountID
	// Manually set active to "upstream" (simulating prior selection).
	q.activeSource[testAccountID] = "upstream"
	sess.Account.AvatarSource = pgtype.Text{String: "upstream", Valid: true}
	sess.Account.AvatarEtag = pgtype.Text{String: "upstream-etag-sticky", Valid: true}
	sess.Account.AvatarContentType = pgtype.Text{String: "image/webp", Valid: true}

	// DELETE the user upload.
	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)

	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE status: want 204, got %d; body=%s", dw.Code, dw.Body.String())
	}
	// User row must be gone.
	if _, ok := q.store[sourceKey{testAccountID, "user"}]; ok {
		t.Error("user row must be deleted")
	}
	// Active source in DB must remain "upstream".
	if q.activeSource[testAccountID] != "upstream" {
		t.Errorf("active source: want upstream (unchanged), got %q", q.activeSource[testAccountID])
	}
	// Session must not have been clobbered.
	if sess.Account.AvatarSource.String != "upstream" {
		t.Errorf("sess AvatarSource: want upstream (unchanged), got %q", sess.Account.AvatarSource.String)
	}
	if !sess.Account.AvatarEtag.Valid || sess.Account.AvatarEtag.String != "upstream-etag-sticky" {
		t.Errorf("sess AvatarEtag: want upstream-etag-sticky (unchanged), got %v", sess.Account.AvatarEtag)
	}

	// Subsequent GET /avatar/{subject} must still serve the upstream image.
	gr := getAvatarReq(t, testSubject, "", "")
	gw := httptest.NewRecorder()
	s.handleGetAvatarHTTP(gw, gr)

	if gw.Code != http.StatusOK {
		t.Fatalf("GET after DELETE: want 200, got %d; body=%s", gw.Code, gw.Body.String())
	}
	if gw.Body.String() != "fake-upstream-webp" {
		t.Errorf("GET body: want fake-upstream-webp, got %q", gw.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /me/avatar/status — pending / not-pending
// ---------------------------------------------------------------------------

// newAvatarStatusTestServer builds a minimal *Server with a real Federator backed
// by a memory KV, suitable for testing handleAvatarStatusHTTP.
func newAvatarStatusTestServer(t *testing.T) (*Server, kv.Store) {
	t.Helper()
	kvStore := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvStore.Close() })

	fq := newFakeFedQueries()
	aud := audit.NewWriter(fq)

	fed := fedoidc.NewFederator(
		fq,
		kvStore,
		aud,
		configx.FederationConfig{
			StateTTL:      5 * time.Minute,
			DefaultScopes: []string{"openid"},
		},
		map[int][]byte{1: make([]byte, 32)},
		nil,
		"",
	)

	s := &Server{
		federator: fed,
		// dbPool nil — status handler doesn't touch DB.
	}
	return s, kvStore
}

// statusReq builds a GET request for /me/avatar/status attached to the given session.
func statusReq(sess *authn.Session) *http.Request {
	r := httptest.NewRequest("GET", "/api/prohibitorum/me/avatar/status", nil)
	if sess != nil {
		r = r.WithContext(authn.WithSession(r.Context(), sess))
	}
	return r
}

// TestAvatarStatus_PendingTrue verifies that when the AvatarFetchKey is present
// in KV the status endpoint returns {"pending":true}.
func TestAvatarStatus_PendingTrue(t *testing.T) {
	s, kvStore := newAvatarStatusTestServer(t)
	sess := avatarSession(testSubject)

	if err := kvStore.SetEx(context.Background(), fedoidc.AvatarFetchKey(testAccountID), "1", time.Minute); err != nil {
		t.Fatalf("seed KV key: %v", err)
	}

	w := httptest.NewRecorder()
	s.handleAvatarStatusHTTP(w, statusReq(sess))

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	var out map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !out["pending"] {
		t.Errorf("pending: want true, got false")
	}
}

// TestAvatarStatus_PendingFalse verifies that when the AvatarFetchKey is absent
// the status endpoint returns {"pending":false}.
func TestAvatarStatus_PendingFalse(t *testing.T) {
	s, _ := newAvatarStatusTestServer(t)
	sess := avatarSession(testSubject)

	w := httptest.NewRecorder()
	s.handleAvatarStatusHTTP(w, statusReq(sess))

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	var out map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if out["pending"] {
		t.Errorf("pending: want false, got true")
	}
}

// TestAvatarStatus_NilFederator verifies that the status endpoint returns
// {"pending":false} safely when s.federator is nil.
func TestAvatarStatus_NilFederator(t *testing.T) {
	s := &Server{} // no federator
	sess := avatarSession(testSubject)

	w := httptest.NewRecorder()
	s.handleAvatarStatusHTTP(w, statusReq(sess))

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	var out map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if out["pending"] {
		t.Errorf("pending: want false when federator is nil, got true")
	}
}
