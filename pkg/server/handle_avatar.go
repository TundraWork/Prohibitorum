// Package server — handle_avatar.go
//
// Avatar list, upload, selection, delete, and public-fetch endpoints.
//
//   GET    /api/prohibitorum/me/avatar           — authed self; stored sources + active source
//   PUT    /api/prohibitorum/me/avatar           — authed self; upload → user source, active=user
//                                                  only if the user never picked an avatar
//   PUT    /api/prohibitorum/me/avatar/selection — authed self; switch active source, mark picked
//   DELETE /api/prohibitorum/me/avatar           — authed self; delete user upload + fallback
//   GET    /avatar/{subject}                     — public; serves active avatar or ?source= specific
//
// The PUT/DELETE handlers follow the same transaction pattern as the other
// sensitive /me mutations: when s.dbPool is nil (unit-test seam), the writes
// run without a transaction through avatarQ(); in production they run inside a
// pgx transaction so both the source upsert and the meta update are atomic.
//
// Error codes avatar_too_large, avatar_invalid_image, and avatar_source_unavailable
// are project-local codes that map to HTTP 400. They are emitted in the JSON
// error envelope as {code, message, details}.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// avatarQueries is the narrow DB surface the avatar handlers require.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring leaves avatarQueriesOverride nil; handlers fall back
// to s.queries.
type avatarQueries interface {
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	MarkAvatarSelected(ctx context.Context, id int32) error
	UpsertAvatarSource(ctx context.Context, arg db.UpsertAvatarSourceParams) error
	SetActiveAvatar(ctx context.Context, arg db.SetActiveAvatarParams) error
	ClearActiveAvatar(ctx context.Context, arg db.ClearActiveAvatarParams) error
	DeleteAvatarSource(ctx context.Context, arg db.DeleteAvatarSourceParams) error
	GetActiveAvatarBySubject(ctx context.Context, oidcSubject pgtype.UUID) (db.GetActiveAvatarBySubjectRow, error)
	GetAvatarSourceBySubject(ctx context.Context, arg db.GetAvatarSourceBySubjectParams) (db.GetAvatarSourceBySubjectRow, error)
	ListAvatarSourcesByAccount(ctx context.Context, accountID int32) ([]db.ListAvatarSourcesByAccountRow, error)
}

func (s *Server) avatarQ() avatarQueries {
	if s.avatarQueriesOverride != nil {
		return s.avatarQueriesOverride
	}
	return s.queries
}

// runAvatarWrite runs fn against the avatar queries, inside a transaction when
// a pool is configured and directly through avatarQ() in the unit-test seam.
func (s *Server) runAvatarWrite(ctx context.Context, fn func(avatarQueries) error) error {
	if s.dbPool == nil {
		return fn(s.avatarQ())
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// errAvatarSourceMissing reports a selection of a source with no stored image.
var errAvatarSourceMissing = errors.New("avatar: no stored image for that source")

type myAvatarOut struct {
	Body contract.MyAvatarView
}

// GET /api/prohibitorum/me/avatar
func (s *Server) handleGetMyAvatar(ctx context.Context, _ *struct{}) (*myAvatarOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.avatarQ().ListAvatarSourcesByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleGetMyAvatar: %w", err)
	}
	origin := ""
	if s.config != nil && len(s.config.PublicOrigins) > 0 {
		origin = s.config.PublicOrigins[0]
	}
	view := contract.MyAvatarView{ActiveSource: "none", Sources: []contract.MyAvatarSourceView{}}
	if sess.Account.AvatarSource.Valid {
		view.ActiveSource = sess.Account.AvatarSource.String
	}
	subject := sess.Account.OidcSubject.String()
	for _, row := range rows {
		u := avatar.SourceURL(subject, row.Source, row.Etag.String, origin)
		if u == "" {
			continue
		}
		view.Sources = append(view.Sources, contract.MyAvatarSourceView{
			Source: row.Source, Label: row.IdpDisplayName, URL: u,
		})
	}
	return &myAvatarOut{Body: view}, nil
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

	upsertArg := db.UpsertAvatarSourceParams{
		AccountID:   acctID,
		Source:      "user",
		Bytes:       out,
		ContentType: ct,
		Etag:        etagPG,
	}

	// The upload only takes effect on its own while the user has never picked
	// an avatar; afterwards their choice stays until they pick again.
	activated := false
	if err := s.runAvatarWrite(ctx, func(q avatarQueries) error {
		if err := q.UpsertAvatarSource(ctx, upsertArg); err != nil {
			return err
		}
		current, err := q.GetAccountByID(ctx, acctID)
		if err != nil {
			return err
		}
		if current.AvatarSelectedAt.Valid {
			return nil
		}
		activated = true
		return q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{Source: "user", AccountID: acctID})
	}); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Mutate in-memory so subsequent /me in the same session sees the new state.
	if activated {
		sess.Account.AvatarSource = pgtype.Text{String: "user", Valid: true}
		sess.Account.AvatarContentType = ct
		sess.Account.AvatarEtag = etagPG
	}

	{
		acctID := acctID
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID: &acctID,
			Factor:    audit.FactorAccount,
			Event:     audit.EventUpdate,
			Detail:    map[string]any{"reason": "avatar_upload", "activated": activated},
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/prohibitorum/me/avatar/selection
func (s *Server) handlePutAvatarSelectionHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}

	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	src := body.Source
	// Accept the user upload, the "none" sentinel, or any per-upstream source
	// ("upstream:<slug>"). Shape is validated here; actual existence is proven
	// by the GetAvatarSourceBySubject lookup below (the "none" branch needs none).
	if src != "user" && src != "none" && !strings.HasPrefix(src, "upstream:") {
		writeAvatarErr(w, "avatar_source_unavailable", "unknown source")
		return
	}

	acctID := sess.Account.ID

	// For "upstream" or "user" the GetAvatarSourceBySubject lookup proves the
	// source exists in the same transaction as the pointer update, so the
	// existence proof and the switch are atomic. Either way the pick is
	// recorded, which stops uploads and upstream avatars taking effect on their
	// own from now on.
	var row db.GetAvatarSourceBySubjectRow
	err := s.runAvatarWrite(ctx, func(q avatarQueries) error {
		if src == "none" {
			if err := q.ClearActiveAvatar(ctx, db.ClearActiveAvatarParams{Source: "none", AccountID: acctID}); err != nil {
				return err
			}
		} else {
			var gerr error
			row, gerr = q.GetAvatarSourceBySubject(ctx, db.GetAvatarSourceBySubjectParams{
				OidcSubject: sess.Account.OidcSubject,
				Source:      src,
			})
			if errors.Is(gerr, pgx.ErrNoRows) {
				return errAvatarSourceMissing
			}
			if gerr != nil {
				return gerr
			}
			if err := q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{Source: src, AccountID: acctID}); err != nil {
				return err
			}
		}
		return q.MarkAvatarSelected(ctx, acctID)
	})
	if errors.Is(err, errAvatarSourceMissing) {
		writeAvatarErr(w, "avatar_source_unavailable", err.Error())
		return
	}
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	sess.Account.AvatarSource = pgtype.Text{String: src, Valid: true}
	sess.Account.AvatarEtag = row.Etag
	sess.Account.AvatarContentType = row.ContentType
	if !sess.Account.AvatarSelectedAt.Valid {
		sess.Account.AvatarSelectedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	{
		acctIDAudit := acctID
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID: &acctIDAudit,
			Factor:    audit.FactorAccount,
			Event:     audit.EventUpdate,
			Detail:    map[string]any{"reason": "avatar_select"},
		})
	}
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
	priorActive := sess.Account.AvatarSource

	deleteArg := db.DeleteAvatarSourceParams{AccountID: acctID, Source: "user"}

	if s.dbPool == nil {
		// Unit-test seam.
		q := s.avatarQ()
		if err := q.DeleteAvatarSource(ctx, deleteArg); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := s.applyDeleteFallback(ctx, q, acctID, priorActive, sess); err != nil {
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
		if err := qtx.DeleteAvatarSource(ctx, deleteArg); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := s.applyDeleteFallback(ctx, qtx, acctID, priorActive, sess); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeAuthErr(w, err)
			return
		}
	}

	{
		acctIDAudit := acctID
		audit.RecordOrLog(ctx, s.Audit, audit.Record{
			AccountID: &acctIDAudit,
			Factor:    audit.FactorAccount,
			Event:     audit.EventUpdate,
			Detail:    map[string]any{"reason": "avatar_remove"},
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// applyDeleteFallback sets active to upstream (if available) or none after the
// user upload has been deleted. It also refreshes sess.
func (s *Server) applyDeleteFallback(
	ctx context.Context,
	q avatarQueries,
	acctID int32,
	priorActive pgtype.Text,
	sess *authn.Session,
) error {
	if priorActive.Valid && priorActive.String == "user" {
		// Look for an inherited upstream fallback. With multiple linked upstreams
		// there may be several "upstream:<slug>" rows; pick the first one found
		// (order is unspecified — the picker is the real re-selection surface).
		srcs, err := q.ListAvatarSourcesByAccount(ctx, acctID)
		if err != nil {
			return err
		}
		var upstreamSource string
		var upstreamEtag pgtype.Text
		for _, row := range srcs {
			if strings.HasPrefix(row.Source, "upstream:") {
				upstreamSource = row.Source
				upstreamEtag = row.Etag
				break
			}
		}
		if upstreamSource != "" {
			if err := q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{Source: upstreamSource, AccountID: acctID}); err != nil {
				return err
			}
			sess.Account.AvatarSource = pgtype.Text{String: upstreamSource, Valid: true}
			sess.Account.AvatarEtag = upstreamEtag
			sess.Account.AvatarContentType = pgtype.Text{String: "image/webp", Valid: true}
		} else {
			if err := q.ClearActiveAvatar(ctx, db.ClearActiveAvatarParams{Source: "none", AccountID: acctID}); err != nil {
				return err
			}
			sess.Account.AvatarSource = pgtype.Text{String: "none", Valid: true}
			sess.Account.AvatarEtag = pgtype.Text{}
			sess.Account.AvatarContentType = pgtype.Text{}
		}
	}
	// Active was not "user" — deletion doesn't change the active pointer;
	// leave the session fields untouched.
	return nil
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

	// Shared response fields extracted from either query result.
	var (
		imgBytes []byte
		ct       pgtype.Text
		etag     pgtype.Text
		disabled bool
	)

	src := r.URL.Query().Get("source")
	if src != "" {
		row, err := s.avatarQ().GetAvatarSourceBySubject(ctx, db.GetAvatarSourceBySubjectParams{
			OidcSubject: subUUID,
			Source:      src,
		})
		if err != nil {
			http.NotFound(w, r)
			return
		}
		imgBytes = row.Bytes
		ct = row.ContentType
		etag = row.Etag
		disabled = row.Disabled
	} else {
		row, err := s.avatarQ().GetActiveAvatarBySubject(ctx, subUUID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		imgBytes = row.Bytes
		ct = row.ContentType
		etag = row.Etag
		disabled = row.Disabled
	}

	if disabled || len(imgBytes) == 0 || !etag.Valid {
		http.NotFound(w, r)
		return
	}

	ctStr := ct.String
	if ctStr == "" {
		ctStr = "image/webp"
	}
	quotedEtag := `"` + etag.String + `"`

	if r.Header.Get("If-None-Match") == quotedEtag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", ctStr)
	w.Header().Set("ETag", quotedEtag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(imgBytes)
}
