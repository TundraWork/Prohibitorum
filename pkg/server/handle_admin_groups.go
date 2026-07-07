// Package server — handle_admin_groups.go
//
// Admin group endpoints:
//   GET  /groups                     — list all groups with member counts (admin, no sudo)
//   GET  /groups/{id}                — get one group (admin, no sudo)
//   GET  /groups/{id}/members        — list group members (admin, no sudo)
//   POST /groups                     — create a group (admin + sudo)
//   PUT  /groups/{id}                — update a group (admin + sudo)
//   POST /groups/delete              — hard-delete a group (admin + sudo)
//   POST /groups/{id}/members        — add a member (admin + sudo)
//   POST /groups/{id}/members/remove — remove a member (admin + sudo)
//
// Mutations are registered via s.registerSudoOpHTTP — the sudo gate, content-type
// check, and body-size limit are all enforced by the wrapper; handlers must NOT
// call requireFreshSudo themselves.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// slugRe is the valid group-slug pattern: lowercase alphanumeric segments
// optionally separated by single hyphens, no leading/trailing hyphen.
var slugRe = regexp.MustCompile(`^[a-z0-9](-?[a-z0-9])*$`)

// validateSlug returns ErrBadRequest if the slug is empty, exceeds 64 chars,
// or does not match slugRe.
func validateSlug(s string) error {
	if s == "" || len(s) > 64 || !slugRe.MatchString(s) {
		return authn.ErrBadRequest()
	}
	return nil
}

// groupView projects a db.UserGroup row + member count into the wire-safe
// contract view.
func groupView(g db.UserGroup, memberCount int64) contract.GroupView {
	v := contract.GroupView{
		ID:                  g.ID,
		Slug:                g.Slug,
		DisplayName:         g.DisplayName,
		ExposedToDownstream: g.ExposedToDownstream,
		MemberCount:         memberCount,
	}
	if g.Description.Valid {
		v.Description = g.Description.String
	}
	if g.CreatedAt.Valid {
		v.CreatedAt = g.CreatedAt.Time
	}
	return v
}

// ----- GET /groups (typed, role-only) ------------------------------------------------

type listGroupsOut struct {
	Body []contract.GroupView
}

func (s *Server) handleListGroups(ctx context.Context, _ *struct{}) (*listGroupsOut, error) {
	rows, err := s.queries.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("handleListGroups: query: %w", err)
	}
	views := make([]contract.GroupView, 0, len(rows))
	for _, r := range rows {
		g := db.UserGroup{
			ID:                  r.ID,
			Slug:                r.Slug,
			DisplayName:         r.DisplayName,
			Description:         r.Description,
			ExposedToDownstream: r.ExposedToDownstream,
			CreatedAt:           r.CreatedAt,
			UpdatedAt:           r.UpdatedAt,
		}
		views = append(views, groupView(g, r.MemberCount))
	}
	return &listGroupsOut{Body: views}, nil
}

// ----- GET /groups/{id} (typed, role-only) -------------------------------------------

type getGroupIn struct {
	ID int32 `path:"id"`
}

type getGroupOut struct {
	Body contract.GroupView
}

func (s *Server) handleGetGroup(ctx context.Context, in *getGroupIn) (*getGroupOut, error) {
	g, err := s.queries.GetGroup(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrGroupNotFound())
		}
		return nil, fmt.Errorf("handleGetGroup: query: %w", err)
	}
	return &getGroupOut{Body: groupView(g, 0)}, nil
}

// ----- GET /groups/{id}/members (typed, role-only) -----------------------------------

type listGroupMembersIn struct {
	ID int32 `path:"id"`
}

type listGroupMembersOut struct {
	Body []contract.GroupMemberView
}

func (s *Server) handleListGroupMembers(ctx context.Context, in *listGroupMembersIn) (*listGroupMembersOut, error) {
	// Pre-check: return 404 for a nonexistent group rather than 200 [].
	if _, err := s.queries.GetGroup(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrGroupNotFound())
		}
		return nil, fmt.Errorf("handleListGroupMembers: existence check: %w", err)
	}

	rows, err := s.queries.ListGroupMembers(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListGroupMembers: query: %w", err)
	}
	views := make([]contract.GroupMemberView, 0, len(rows))
	for _, r := range rows {
		views = append(views, contract.GroupMemberView{
			ID:          r.ID,
			Username:    r.Username,
			DisplayName: r.DisplayName,
		})
	}
	return &listGroupMembersOut{Body: views}, nil
}

// ----- POST /groups (raw, sudo-gated) ------------------------------------------------

type createGroupBody struct {
	Slug                string `json:"slug"`
	DisplayName         string `json:"displayName"`
	Description         string `json:"description"`
	ExposedToDownstream *bool  `json:"exposedToDownstream"`
}

func (s *Server) handleCreateGroupHTTP(w http.ResponseWriter, r *http.Request) {
	var body createGroupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := validateSlug(body.Slug); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Default exposedToDownstream to true when not provided.
	exposed := true
	if body.ExposedToDownstream != nil {
		exposed = *body.ExposedToDownstream
	}

	var desc pgtype.Text
	if body.Description != "" {
		desc = pgtype.Text{String: body.Description, Valid: true}
	}

	g, err := s.queries.CreateGroup(r.Context(), db.CreateGroupParams{
		Slug:                body.Slug,
		DisplayName:         body.DisplayName,
		Description:         desc,
		ExposedToDownstream: exposed,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrGroupSlugConflict())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateGroup: insert: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorGroup,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"group_id": g.ID, "slug": g.Slug},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(groupView(g, 0))
}

// ----- PUT /groups/{id} (raw, sudo-gated) --------------------------------------------

type updateGroupBody struct {
	Slug                string `json:"slug"`
	DisplayName         string `json:"displayName"`
	Description         string `json:"description"`
	ExposedToDownstream *bool  `json:"exposedToDownstream"`
}

func (s *Server) handleUpdateGroupHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id64, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id := int32(id64)

	var body updateGroupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if err := validateSlug(body.Slug); err != nil {
		writeAuthErr(w, err)
		return
	}

	// Read the current row to verify existence and to preserve ExposedToDownstream
	// when the caller omits the field (nil pointer = "not provided").
	current, err := s.queries.GetGroup(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrGroupNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateGroup: get: %w", err))
		return
	}

	exposed := current.ExposedToDownstream
	if body.ExposedToDownstream != nil {
		exposed = *body.ExposedToDownstream
	}

	var desc pgtype.Text
	if body.Description != "" {
		desc = pgtype.Text{String: body.Description, Valid: true}
	}

	g, err := s.queries.UpdateGroup(r.Context(), db.UpdateGroupParams{
		ID:                  id,
		Slug:                body.Slug,
		DisplayName:         body.DisplayName,
		Description:         desc,
		ExposedToDownstream: exposed,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrGroupNotFound())
			return
		}
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrGroupSlugConflict())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateGroup: update: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorGroup,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"group_id": id, "slug": body.Slug},
	})

	writeJSON(w, groupView(g, 0))
}

// ----- POST /groups/delete (raw, sudo-gated) -----------------------------------------

type deleteGroupBody struct {
	ID int32 `json:"id"`
}

func (s *Server) handleDeleteGroupHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteGroupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	// Load the group slug before the hard-delete so the audit record carries a
	// human-readable identifier (group rows are unrecoverable after deletion).
	var groupSlug string
	if g, err := s.queries.GetGroup(r.Context(), body.ID); err == nil {
		groupSlug = g.Slug
	}

	rows, err := s.queries.DeleteGroup(r.Context(), body.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteGroup: delete: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrGroupNotFound())
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorGroup,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"group_id": body.ID, "slug": groupSlug},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /groups/{id}/members (raw, sudo-gated) -----------------------------------

type addGroupMemberBody struct {
	AccountID int32 `json:"accountId"`
}

func (s *Server) handleAddGroupMemberHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id64, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	groupID := int32(id64)

	var body addGroupMemberBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	if err := s.queries.AddGroupMember(r.Context(), db.AddGroupMemberParams{
		GroupID:   groupID,
		AccountID: body.AccountID,
	}); err != nil {
		if isForeignKeyViolation(err) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleAddGroupMember: insert: %w", err))
		return
	}

	sess := authn.SessionFromContext(r.Context())
	var actorID *int32
	if sess != nil {
		actorID = &sess.Account.ID
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: actorID,
		Factor:    audit.FactorGroup,
		Event:     audit.EventLink,
		Detail:    map[string]any{"group_id": groupID, "account_id": body.AccountID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ----- POST /groups/{id}/members/remove (raw, sudo-gated) ----------------------------

type removeGroupMemberBody struct {
	AccountID int32 `json:"accountId"`
}

func (s *Server) handleRemoveGroupMemberHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	id64, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	groupID := int32(id64)

	var body removeGroupMemberBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	rows, err := s.queries.RemoveGroupMember(r.Context(), db.RemoveGroupMemberParams{
		GroupID:   groupID,
		AccountID: body.AccountID,
	})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRemoveGroupMember: delete: %w", err))
		return
	}

	// Only write an audit record when a membership row was actually deleted;
	// skip on the no-op path (membership didn't exist) to avoid misleading audit
	// entries. The 204 response is returned regardless — removal is idempotent.
	if rows > 0 {
		sess := authn.SessionFromContext(r.Context())
		var actorID *int32
		if sess != nil {
			actorID = &sess.Account.ID
		}
		_ = s.Audit.Record(r.Context(), audit.Record{
			AccountID: actorID,
			Factor:    audit.FactorGroup,
			Event:     audit.EventUnlink,
			Detail:    map[string]any{"group_id": groupID, "account_id": body.AccountID},
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
