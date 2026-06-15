// Package server — handle_admin_groups_test.go
//
// Unit tests for the group admin surface. These tests are DB-free: the view
// projection (groupView) and the slug validator are the primary units under test.
// Route-level sudo gating is covered centrally in admin_route_policy_test.go.

package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// ----- groupView projection tests ---------------------------------------------------

// TestAdminGroups_ViewProjection_FieldMapping verifies that all fields from a
// fully-populated db.UserGroup row are correctly mapped into the wire view.
func TestAdminGroups_ViewProjection_FieldMapping(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	row := db.UserGroup{
		ID:                  42,
		Slug:                "eng-backend",
		DisplayName:         "Engineering Backend",
		Description:         pgtype.Text{String: "Backend engineers", Valid: true},
		ExposedToDownstream: true,
		CreatedAt:           pgtype.Timestamptz{Time: createdAt, Valid: true},
	}

	view := groupView(row, 7)

	if view.ID != 42 {
		t.Errorf("ID: got %d, want 42", view.ID)
	}
	if view.Slug != "eng-backend" {
		t.Errorf("Slug: got %q, want eng-backend", view.Slug)
	}
	if view.DisplayName != "Engineering Backend" {
		t.Errorf("DisplayName: got %q, want Engineering Backend", view.DisplayName)
	}
	if view.Description != "Backend engineers" {
		t.Errorf("Description: got %q, want Backend engineers", view.Description)
	}
	if !view.ExposedToDownstream {
		t.Error("ExposedToDownstream: got false, want true")
	}
	if view.MemberCount != 7 {
		t.Errorf("MemberCount: got %d, want 7", view.MemberCount)
	}
	if !view.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt: got %v, want %v", view.CreatedAt, createdAt)
	}
}

// TestAdminGroups_ViewProjection_NullDescription verifies that a NULL description
// (Valid=false) produces an empty string in the view (omitempty serialises it away).
func TestAdminGroups_ViewProjection_NullDescription(t *testing.T) {
	t.Parallel()

	row := db.UserGroup{
		ID:          1,
		Slug:        "ops",
		DisplayName: "Ops",
		// Description intentionally zero value (Valid=false)
	}

	view := groupView(row, 0)

	if view.Description != "" {
		t.Errorf("Description: got %q, want empty string for NULL column", view.Description)
	}
}

// TestAdminGroups_ViewProjection_InvalidTimestamp verifies that a NULL CreatedAt
// yields a zero-value time.Time rather than panicking.
func TestAdminGroups_ViewProjection_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	row := db.UserGroup{
		ID:          3,
		Slug:        "test",
		DisplayName: "Test",
		// CreatedAt intentionally left as zero value (Valid=false)
	}

	view := groupView(row, 0)

	if !view.CreatedAt.IsZero() {
		t.Errorf("CreatedAt: got %v, want zero time for invalid column", view.CreatedAt)
	}
}

// TestAdminGroups_ViewProjection_ExposedFalse verifies that ExposedToDownstream=false
// is correctly propagated (not silently defaulted to true).
func TestAdminGroups_ViewProjection_ExposedFalse(t *testing.T) {
	t.Parallel()

	row := db.UserGroup{
		ID:                  9,
		Slug:                "internal",
		DisplayName:         "Internal",
		ExposedToDownstream: false,
	}

	view := groupView(row, 0)

	if view.ExposedToDownstream {
		t.Error("ExposedToDownstream: got true, want false")
	}
}

// ----- validateSlug tests -----------------------------------------------------------

// TestAdminGroups_ValidateSlug_Valid verifies that well-formed slugs pass validation.
func TestAdminGroups_ValidateSlug_Valid(t *testing.T) {
	t.Parallel()

	valid := []string{
		"a",
		"abc",
		"eng-backend",
		"team-1",
		"a1b2c3",
		"x-y-z",
	}
	for _, s := range valid {
		if err := validateSlug(s); err != nil {
			t.Errorf("validateSlug(%q) = %v, want nil", s, err)
		}
	}
}

// TestAdminGroups_ValidateSlug_Invalid verifies that malformed slugs are rejected.
func TestAdminGroups_ValidateSlug_Invalid(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",                           // empty
		"-leading",                   // leading hyphen
		"trailing-",                  // trailing hyphen
		"double--hyphen",             // consecutive hyphens
		"UPPERCASE",                  // uppercase
		"has space",                  // space
		"has_underscore",             // underscore
		"has.dot",                    // dot
	}
	for _, s := range invalid {
		if err := validateSlug(s); err == nil {
			t.Errorf("validateSlug(%q) = nil, want error", s)
		}
	}
}

// TestAdminGroups_ValidateSlug_TooLong verifies that a slug exceeding 64 chars
// is rejected.
func TestAdminGroups_ValidateSlug_TooLong(t *testing.T) {
	t.Parallel()

	// 65 lowercase alpha chars — valid pattern but too long.
	long := "a"
	for i := 0; i < 64; i++ {
		long += "b"
	}
	if err := validateSlug(long); err == nil {
		t.Errorf("validateSlug(65-char slug) = nil, want error")
	}
	// Exactly 64 chars is fine.
	exact := long[:64]
	if err := validateSlug(exact); err != nil {
		t.Errorf("validateSlug(64-char slug) = %v, want nil", err)
	}
}

// ----- GroupMemberView construction test --------------------------------------------

// TestAdminGroups_MemberViewMapping verifies that ListGroupMembersRow fields
// map to GroupMemberView correctly (the handler assembles this inline).
func TestAdminGroups_MemberViewMapping(t *testing.T) {
	t.Parallel()

	row := db.ListGroupMembersRow{
		ID:          101,
		Username:    "alice",
		DisplayName: "Alice Smith",
	}

	view := contract.GroupMemberView{
		ID:          row.ID,
		Username:    row.Username,
		DisplayName: row.DisplayName,
	}

	if view.ID != 101 {
		t.Errorf("ID: got %d, want 101", view.ID)
	}
	if view.Username != "alice" {
		t.Errorf("Username: got %q, want alice", view.Username)
	}
	if view.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName: got %q, want Alice Smith", view.DisplayName)
	}
}
