// Package server — handle_avatar_test.go
//
// Unit tests for PUT /api/prohibitorum/me/avatar, DELETE /api/prohibitorum/me/avatar,
// and GET /avatar/{subject}.
//
// All tests use a fake avatarQueries stub (no real DB). The production
// transaction path is exercised by smoke tests; unit tests focus on the
// handler logic, error codes, ETag conditional-GET, and the nil-dbPool seam.

package server

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	_ "github.com/gen2brain/webp" // register webp decoder for image.DecodeConfig

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// ---------------------------------------------------------------------------
// Fake avatarQueries
// ---------------------------------------------------------------------------

type fakeAvatarQueries struct {
	// in-memory avatar store: keyed by accountID
	avatarBytes map[int32][]byte
	avatarMeta  map[int32]db.SetAccountAvatarMetaParams
	// for GetAvatarBySubject: keyed by subject UUID string
	subjectMap map[string]int32 // subject → accountID
	disabled   map[int32]bool
}

func newFakeAvatarQ() *fakeAvatarQueries {
	return &fakeAvatarQueries{
		avatarBytes: make(map[int32][]byte),
		avatarMeta:  make(map[int32]db.SetAccountAvatarMetaParams),
		subjectMap:  make(map[string]int32),
		disabled:    make(map[int32]bool),
	}
}

func (f *fakeAvatarQueries) UpsertAccountAvatarBytes(_ context.Context, arg db.UpsertAccountAvatarBytesParams) error {
	f.avatarBytes[arg.AccountID] = arg.Bytes
	return nil
}

func (f *fakeAvatarQueries) SetAccountAvatarMeta(_ context.Context, arg db.SetAccountAvatarMetaParams) error {
	f.avatarMeta[arg.ID] = arg
	return nil
}

func (f *fakeAvatarQueries) ClearAccountAvatarBytes(_ context.Context, accountID int32) error {
	delete(f.avatarBytes, accountID)
	return nil
}

func (f *fakeAvatarQueries) ClearAccountAvatarMeta(_ context.Context, id int32) error {
	delete(f.avatarMeta, id)
	return nil
}

func (f *fakeAvatarQueries) GetAvatarBySubject(_ context.Context, sub pgtype.UUID) (db.GetAvatarBySubjectRow, error) {
	subStr := sub.String()
	acctID, ok := f.subjectMap[subStr]
	if !ok {
		return db.GetAvatarBySubjectRow{}, errNoRows
	}
	b, hasBytes := f.avatarBytes[acctID]
	m, hasMeta := f.avatarMeta[acctID]
	if !hasBytes || !hasMeta {
		return db.GetAvatarBySubjectRow{}, errNoRows
	}
	return db.GetAvatarBySubjectRow{
		Bytes:             b,
		AvatarContentType: m.AvatarContentType,
		AvatarEtag:        m.AvatarEtag,
		Disabled:          f.disabled[acctID],
	}, nil
}

// errNoRows is used by the fake to signal "not found".
// We re-use the pgx sentinel so the handler's `err != nil` check fires.
var errNoRows = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

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
func getAvatarReq(t *testing.T, sub string, ifNoneMatch string) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/avatar/"+sub, nil)
	if ifNoneMatch != "" {
		r.Header.Set("If-None-Match", ifNoneMatch)
	}
	// Install chi route context so chi.URLParam works without a real router.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("subject", sub)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// decodeAvatarJSON returns the "code" field from a JSON error body.
func decodeAvatarErrCode(t *testing.T, body string) string {
	t.Helper()
	m := decodeJSON(t, []byte(body))
	code, _ := m["code"].(string)
	return code
}

// ---------------------------------------------------------------------------
// PUT /me/avatar tests
// ---------------------------------------------------------------------------

// TestPutAvatar_ValidPNG_Stores204 verifies that uploading a valid PNG produces
// a 204, stores avatar bytes, and sets the etag on the in-memory session.
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
	if len(q.avatarBytes[testAccountID]) == 0 {
		t.Error("no avatar bytes stored")
	}
	meta, ok := q.avatarMeta[testAccountID]
	if !ok {
		t.Fatal("no avatar meta stored")
	}
	if !meta.AvatarEtag.Valid || meta.AvatarEtag.String == "" {
		t.Error("avatar_etag must be set")
	}
	if meta.AvatarContentType.String != "image/webp" {
		t.Errorf("content_type: want image/webp, got %q", meta.AvatarContentType.String)
	}
	// In-memory session should reflect the new etag.
	if !sess.Account.AvatarEtag.Valid {
		t.Error("sess.Account.AvatarEtag must be valid after PUT")
	}
}

// TestPutAvatar_StoresWebP512 verifies that the stored bytes decode as a
// 512×512 WebP image.
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
	stored := q.avatarBytes[testAccountID]
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
// DELETE /me/avatar tests
// ---------------------------------------------------------------------------

// TestDeleteAvatar_204_ClearsStore verifies DELETE returns 204 and removes
// the stored bytes and meta.
func TestDeleteAvatar_204_ClearsStore(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)

	// First PUT to seed data.
	pr := putAvatarReq(t, smallPNG(t), sess)
	pw := httptest.NewRecorder()
	s.handlePutAvatarHTTP(pw, pr)
	if pw.Code != http.StatusNoContent {
		t.Fatalf("seed PUT: want 204, got %d", pw.Code)
	}

	// Now DELETE.
	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)

	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE status: want 204, got %d; body=%s", dw.Code, dw.Body.String())
	}
	if len(q.avatarBytes[testAccountID]) != 0 {
		t.Error("avatar bytes must be cleared after DELETE")
	}
	if _, ok := q.avatarMeta[testAccountID]; ok {
		t.Error("avatar meta must be cleared after DELETE")
	}
	if sess.Account.AvatarEtag.Valid {
		t.Error("sess.Account.AvatarEtag must be cleared after DELETE")
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

// seedAvatar uploads an avatar via PUT, then registers the subject → accountID
// mapping in the fake so GetAvatarBySubject can find it.
func seedAvatar(t *testing.T, s *Server, q *fakeAvatarQueries, sess *authn.Session) string {
	t.Helper()
	pr := putAvatarReq(t, smallPNG(t), sess)
	pw := httptest.NewRecorder()
	s.handlePutAvatarHTTP(pw, pr)
	if pw.Code != http.StatusNoContent {
		t.Fatalf("seed PUT: want 204, got %d; body=%s", pw.Code, pw.Body.String())
	}
	// Register subject in fake so GetAvatarBySubject resolves it.
	q.subjectMap[testSubject] = testAccountID
	return q.avatarMeta[testAccountID].AvatarEtag.String
}

// TestGetAvatar_200_ValidBytes verifies GET returns 200, Content-Type: image/webp,
// a non-empty ETag, and bytes that decode as a 512×512 WebP.
func TestGetAvatar_200_ValidBytes(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	etag := seedAvatar(t, s, q, sess)

	r := getAvatarReq(t, testSubject, "")
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
	_ = etag // verified via ETag header above
}

// TestGetAvatar_304_IfNoneMatch verifies that If-None-Match with the current
// ETag returns 304 with no body.
func TestGetAvatar_304_IfNoneMatch(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	etag := seedAvatar(t, s, q, sess)
	quotedEtag := `"` + etag + `"`

	r := getAvatarReq(t, testSubject, quotedEtag)
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

	r := getAvatarReq(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "")
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

	r := getAvatarReq(t, "not-a-uuid", "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestGetAvatar_404_BeforeUpload verifies that GET before any avatar upload
// returns 404 even when the account exists.
func TestGetAvatar_404_BeforeUpload(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	// Register the subject → account mapping but do NOT seed any bytes.
	q.subjectMap[testSubject] = testAccountID

	r := getAvatarReq(t, testSubject, "")
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
	seedAvatar(t, s, q, sess)
	q.disabled[testAccountID] = true

	r := getAvatarReq(t, testSubject, "")
	w := httptest.NewRecorder()
	s.handleGetAvatarHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", w.Code)
	}
}

// TestDeleteAvatar_ThenGet_404 verifies that after DELETE, GET returns 404.
func TestDeleteAvatar_ThenGet_404(t *testing.T) {
	q := newFakeAvatarQ()
	s := newAvatarServer(t, q)
	sess := avatarSession(testSubject)
	seedAvatar(t, s, q, sess)

	// DELETE.
	dr := deleteAvatarReq(t, sess)
	dw := httptest.NewRecorder()
	s.handleDeleteAvatarHTTP(dw, dr)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", dw.Code)
	}

	// Subsequent GET.
	gr := getAvatarReq(t, testSubject, "")
	gw := httptest.NewRecorder()
	s.handleGetAvatarHTTP(gw, gr)

	if gw.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: want 404, got %d", gw.Code)
	}
}

// Compile-time check: fakeAvatarQueries must satisfy the avatarQueries interface.
var _ avatarQueries = (*fakeAvatarQueries)(nil)
