package enrollment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// fakeEnrollQ is a hand-rolled in-memory fake that satisfies the db.Querier
// interface — but only the three enrollment methods are implemented. Calling
// any other method panics, which is fine because we never exercise them.
type fakeEnrollQ struct {
	db.Querier             // embedded nil — methods we don't override will panic if called.
	enrollments            map[string]db.Enrollment
	inserted               []db.InsertEnrollmentParams
	insertErr              error
}

func newFakeEnrollQ() *fakeEnrollQ {
	return &fakeEnrollQ{enrollments: map[string]db.Enrollment{}}
}

func (f *fakeEnrollQ) InsertEnrollment(_ context.Context, p db.InsertEnrollmentParams) (db.Enrollment, error) {
	f.inserted = append(f.inserted, p)
	e := db.Enrollment{
		Token:              p.Token,
		Intent:             p.Intent,
		TargetAccountID:    p.TargetAccountID,
		TemplateUsername:   p.TemplateUsername,
		GroupIds:           append([]int32(nil), p.GroupIds...),
		CreatedByAccountID: p.CreatedByAccountID,
		CreatedAt:          pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:          p.ExpiresAt,
	}
	f.enrollments[p.Token] = e
	return e, nil
}

type fakeInvitationGroupApplier struct {
	calls   []db.ApplyInvitationGroupsParams
	applied []int32
	err     error
}

func (f *fakeInvitationGroupApplier) ApplyInvitationGroups(_ context.Context, p db.ApplyInvitationGroupsParams) ([]int32, error) {
	p.GroupIds = append([]int32(nil), p.GroupIds...)
	f.calls = append(f.calls, p)
	return append([]int32(nil), f.applied...), f.err
}

func (f *fakeEnrollQ) GetEnrollmentByToken(_ context.Context, t string) (db.Enrollment, error) {
	if e, ok := f.enrollments[t]; ok {
		return e, nil
	}
	return db.Enrollment{}, pgx.ErrNoRows
}

func (f *fakeEnrollQ) ConsumeEnrollment(_ context.Context, t string) (db.Enrollment, error) {
	e, ok := f.enrollments[t]
	if !ok || e.ConsumedAt.Valid {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	e.ConsumedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	f.enrollments[t] = e
	return e, nil
}

func TestEnrollment_IssueAndLoad(t *testing.T) {
	q := newFakeEnrollQ()
	tok, exp, err := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 {
		t.Errorf("token len = %d, want 43", len(tok))
	}
	if exp.Before(time.Now()) {
		t.Error("expires_at should be in the future")
	}
	e, err := LoadEnrollment(context.Background(), q, tok)
	if err != nil {
		t.Fatal(err)
	}
	if e.Intent != IntentBootstrap {
		t.Errorf("intent = %q, want %q", e.Intent, IntentBootstrap)
	}
	if e.TargetAccountID.Valid {
		t.Error("bootstrap should have null target")
	}
	if q.inserted[0].GroupIds == nil || len(q.inserted[0].GroupIds) != 0 {
		t.Fatalf("bootstrap group IDs = %#v, want a non-nil empty array", q.inserted[0].GroupIds)
	}
}

func TestIssueEnrollment_PersistsInvitationTemplate(t *testing.T) {
	q := newFakeEnrollQ()
	username := "alice"
	creatorID := int32(17)
	_, _, err := IssueEnrollment(context.Background(), q, IntentInvite, nil, time.Hour, &EnrollmentTemplate{
		Role:               "user",
		Username:           &username,
		GroupIDs:           []int32{9, 4, 9},
		CreatedByAccountID: &creatorID,
	})
	if err != nil {
		t.Fatalf("IssueEnrollment: %v", err)
	}
	if len(q.inserted) != 1 {
		t.Fatalf("inserts = %d, want 1", len(q.inserted))
	}
	p := q.inserted[0]
	if !p.TemplateUsername.Valid || p.TemplateUsername.String != username {
		t.Fatalf("template username = %+v, want %q", p.TemplateUsername, username)
	}
	if len(p.GroupIds) != 2 || p.GroupIds[0] != 9 || p.GroupIds[1] != 4 {
		t.Fatalf("group IDs = %v, want [9 4]", p.GroupIds)
	}
	if !p.CreatedByAccountID.Valid || p.CreatedByAccountID.Int32 != creatorID {
		t.Fatalf("creator = %+v, want %d", p.CreatedByAccountID, creatorID)
	}
}

func TestApplyInvitationGroups(t *testing.T) {
	creator := pgtype.Int4{Int32: 17, Valid: true}

	t.Run("empty list is a no-op", func(t *testing.T) {
		q := &fakeInvitationGroupApplier{}
		if err := ApplyInvitationGroups(context.Background(), q, nil, 23, creator); err != nil {
			t.Fatalf("ApplyInvitationGroups: %v", err)
		}
		if len(q.calls) != 0 {
			t.Fatalf("calls = %d, want 0", len(q.calls))
		}
	})

	t.Run("deduplicates and applies every available group", func(t *testing.T) {
		q := &fakeInvitationGroupApplier{applied: []int32{4, 9}}
		if err := ApplyInvitationGroups(context.Background(), q, []int32{9, 4, 9}, 23, creator); err != nil {
			t.Fatalf("ApplyInvitationGroups: %v", err)
		}
		if len(q.calls) != 1 {
			t.Fatalf("calls = %d, want 1", len(q.calls))
		}
		p := q.calls[0]
		if p.AccountID != 23 || p.CreatedBy != creator {
			t.Fatalf("params = %+v", p)
		}
		if len(p.GroupIds) != 2 || p.GroupIds[0] != 9 || p.GroupIds[1] != 4 {
			t.Fatalf("group IDs = %v, want [9 4]", p.GroupIds)
		}
	})

	t.Run("reports only unavailable IDs", func(t *testing.T) {
		q := &fakeInvitationGroupApplier{applied: []int32{4}}
		err := ApplyInvitationGroups(context.Background(), q, []int32{9, 4, 12, 9}, 23, creator)
		authErr := authn.AsAuthError(err)
		if authErr == nil || authErr.Code != "invitation_groups_unavailable" {
			t.Fatalf("error = %v, want invitation_groups_unavailable", err)
		}
		invalid, ok := authErr.Details["groupIds"].([]int32)
		if !ok || len(invalid) != 2 || invalid[0] != 9 || invalid[1] != 12 {
			t.Fatalf("invalid group IDs = %#v, want [9 12]", authErr.Details["groupIds"])
		}
	})

	t.Run("wraps storage failure", func(t *testing.T) {
		storageErr := errors.New("storage unavailable")
		q := &fakeInvitationGroupApplier{err: storageErr}
		err := ApplyInvitationGroups(context.Background(), q, []int32{9}, 23, creator)
		if !errors.Is(err, storageErr) {
			t.Fatalf("error = %v, want wrapped storage failure", err)
		}
	})
}

func TestEnrollment_IssueWithTarget(t *testing.T) {
	q := newFakeEnrollQ()
	tid := int32(42)
	tok, _, err := IssueEnrollment(context.Background(), q, IntentInvite, &tid, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := LoadEnrollment(context.Background(), q, tok)
	if err != nil {
		t.Fatal(err)
	}
	if !e.TargetAccountID.Valid || e.TargetAccountID.Int32 != 42 {
		t.Errorf("target = %+v, want {42, true}", e.TargetAccountID)
	}
}

func TestEnrollment_Consume(t *testing.T) {
	q := newFakeEnrollQ()
	tok, _, _ := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, time.Hour, nil)
	if _, err := ConsumeEnrollment(context.Background(), q, tok); err != nil {
		t.Fatal(err)
	}
	_, err := LoadEnrollment(context.Background(), q, tok)
	if authn.AsAuthError(err) == nil || authn.AsAuthError(err).Code != "enrollment_consumed" {
		t.Errorf("want enrollment_consumed, got %v", err)
	}
}

func TestConsumeEnrollment_OncePerToken(t *testing.T) {
	q := newFakeEnrollQ()
	tok, _, err := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, time.Hour, nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := ConsumeEnrollment(context.Background(), q, tok); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	_, err = ConsumeEnrollment(context.Background(), q, tok)
	if err == nil {
		t.Fatal("second consume should have failed")
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "enrollment_consumed" {
		t.Fatalf("second consume: want enrollment_consumed, got %v", err)
	}
}

func TestEnrollment_Expired(t *testing.T) {
	q := newFakeEnrollQ()
	tok, _, _ := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, -1*time.Hour, nil)
	_, err := LoadEnrollment(context.Background(), q, tok)
	if authn.AsAuthError(err) == nil || authn.AsAuthError(err).Code != "enrollment_expired" {
		t.Errorf("want enrollment_expired, got %v", err)
	}
}

func TestEnrollment_MissingTreatedAsConsumed(t *testing.T) {
	q := newFakeEnrollQ()
	_, err := LoadEnrollment(context.Background(), q, "bogus_never_issued")
	if authn.AsAuthError(err) == nil || authn.AsAuthError(err).Code != "enrollment_consumed" {
		t.Errorf("missing should surface as enrollment_consumed (proxy for cascade-deleted), got %v", err)
	}
}

func TestEnrollment_DefaultTTL(t *testing.T) {
	q := newFakeEnrollQ()
	before := time.Now()
	_, exp, err := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should be approximately DefaultEnrollmentTTL from now.
	diff := exp.Sub(before)
	if diff < DefaultEnrollmentTTL-1*time.Second || diff > DefaultEnrollmentTTL+5*time.Second {
		t.Errorf("default TTL gave expiry %v from now; want ~%v", diff, DefaultEnrollmentTTL)
	}
}

func TestEnrollment_TokenIsURLSafe(t *testing.T) {
	q := newFakeEnrollQ()
	tok, _, _ := IssueEnrollment(context.Background(), q, IntentBootstrap, nil, time.Hour, nil)
	for _, c := range tok {
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			t.Errorf("token contains non-url-safe char %q in %q", c, tok)
			break
		}
	}
}
