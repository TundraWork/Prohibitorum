// Package server — handle_avatar.go
//
// Avatar upload, delete, and public-fetch endpoints.
//
//   PUT    /api/prohibitorum/me/avatar    — authed self; stores/replaces avatar
//   DELETE /api/prohibitorum/me/avatar    — authed self; removes avatar
//   GET    /avatar/{subject}              — public; serves avatar bytes
//
// The PUT/DELETE handlers follow the same transaction pattern as the other
// sensitive /me mutations: when s.dbPool is nil (unit-test seam), the writes
// run without a transaction through avatarQ(); in production they run inside a
// pgx transaction so both the avatar_bytes upsert and the meta update are atomic.
//
// Error codes avatar_too_large and avatar_invalid_image are project-local
// codes that map to HTTP 400. They are emitted in the JSON error envelope as
// {code, message, details}.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
)

// avatarQueries is the narrow DB surface the avatar handlers require.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring leaves avatarQueriesOverride nil; handlers fall back
// to s.queries.
type avatarQueries interface {
	UpsertAccountAvatarBytes(ctx context.Context, arg db.UpsertAccountAvatarBytesParams) error
	SetAccountAvatarMeta(ctx context.Context, arg db.SetAccountAvatarMetaParams) error
	ClearAccountAvatarBytes(ctx context.Context, accountID int32) error
	ClearAccountAvatarMeta(ctx context.Context, id int32) error
	GetAvatarBySubject(ctx context.Context, oidcSubject pgtype.UUID) (db.GetAvatarBySubjectRow, error)
}

func (s *Server) avatarQ() avatarQueries {
	if s.avatarQueriesOverride != nil {
		return s.avatarQueriesOverride
	}
	return s.queries
}

// maxAvatarRead is slightly above avatar.maxInputBytes (5 MiB) so Process can
// distinguish "exactly at limit" from "over limit" without us truncating early.
const maxAvatarRead = 5<<20 + 1

// writeAvatarErr writes a 400 JSON error envelope with the given code, matching
// the project-wide {code, message, details} shape used by writeAuthErr.
func writeAvatarErr(w http.ResponseWriter, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"message": msg,
		"details": []string{},
	})
}

// PUT /api/prohibitorum/me/avatar
func (s *Server) handlePutAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}

	// Read the raw body (capped so we can report avatar_too_large cleanly).
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAvatarRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	out, etag, err := avatar.Process(raw)
	if err != nil {
		if errors.Is(err, avatar.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "avatar: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "avatar: invalid or unsupported image format")
		return
	}

	acctID := sess.Account.ID
	ct := pgtype.Text{String: "image/webp", Valid: true}
	etagPG := pgtype.Text{String: etag, Valid: true}

	if s.dbPool == nil {
		// Unit-test seam: no real pool — run writes without a transaction via
		// avatarQ() (which resolves to the injected fake or s.queries).
		q := s.avatarQ()
		if err := q.UpsertAccountAvatarBytes(ctx, db.UpsertAccountAvatarBytesParams{
			AccountID: acctID,
			Bytes:     out,
		}); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := q.SetAccountAvatarMeta(ctx, db.SetAccountAvatarMetaParams{
			ID:                acctID,
			AvatarContentType: ct,
			AvatarEtag:        etagPG,
		}); err != nil {
			writeAuthErr(w, err)
			return
		}
	} else {
		tx, txErr := s.dbPool.Begin(ctx)
		if txErr != nil {
			writeAuthErr(w, txErr)
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		qtx := s.queries.WithTx(tx)
		if err := qtx.UpsertAccountAvatarBytes(ctx, db.UpsertAccountAvatarBytesParams{
			AccountID: acctID,
			Bytes:     out,
		}); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := qtx.SetAccountAvatarMeta(ctx, db.SetAccountAvatarMetaParams{
			ID:                acctID,
			AvatarContentType: ct,
			AvatarEtag:        etagPG,
		}); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeAuthErr(w, err)
			return
		}
	}

	// Mutate in-memory so subsequent /me in the same session sees the new etag.
	sess.Account.AvatarContentType = ct
	sess.Account.AvatarEtag = etagPG

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/me/avatar
func (s *Server) handleDeleteAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}

	acctID := sess.Account.ID

	if s.dbPool == nil {
		// Unit-test seam: no real pool — run writes without a transaction via
		// avatarQ() (which resolves to the injected fake or s.queries).
		q := s.avatarQ()
		if err := q.ClearAccountAvatarBytes(ctx, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := q.ClearAccountAvatarMeta(ctx, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
	} else {
		tx, txErr := s.dbPool.Begin(ctx)
		if txErr != nil {
			writeAuthErr(w, txErr)
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		qtx := s.queries.WithTx(tx)
		if err := qtx.ClearAccountAvatarBytes(ctx, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := qtx.ClearAccountAvatarMeta(ctx, acctID); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeAuthErr(w, err)
			return
		}
	}

	// Clear in-memory so subsequent /me reflects cleared state.
	sess.Account.AvatarContentType = pgtype.Text{}
	sess.Account.AvatarEtag = pgtype.Text{}

	w.WriteHeader(http.StatusNoContent)
}

// GET /avatar/{subject}  (public, no auth required)
func (s *Server) handleGetAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subStr := chi.URLParam(r, "subject")

	var subUUID pgtype.UUID
	if err := subUUID.Scan(subStr); err != nil {
		http.NotFound(w, r)
		return
	}

	row, err := s.avatarQ().GetAvatarBySubject(ctx, subUUID)
	if err != nil || row.Disabled || len(row.Bytes) == 0 || !row.AvatarEtag.Valid {
		http.NotFound(w, r)
		return
	}

	ct := row.AvatarContentType.String
	if ct == "" {
		ct = "image/webp"
	}
	quotedEtag := `"` + row.AvatarEtag.String + `"`

	if r.Header.Get("If-None-Match") == quotedEtag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("ETag", quotedEtag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(row.Bytes)
}
