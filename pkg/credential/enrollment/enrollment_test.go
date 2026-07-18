package enrollment

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"testing"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// fakeEnrollQ is a hand-rolled in-memory fake that satisfies the db.Querier
// interface — but only the three enrollment methods are implemented. Calling
// any other method panics, which is fine because we never exercise them.
type fakeEnrollQ struct {
	db.Querier             // embedded nil — methods we don't override will panic if called.
	enrollments            map[string]db.Enrollment
	federatedParams        []db.InsertFederatedRegistrationEnrollmentParams
	providerRecoveryParams []db.InsertProviderRecoveryEnrollmentParams
	insertErr              error
}

func newFakeEnrollQ() *fakeEnrollQ {
	return &fakeEnrollQ{enrollments: map[string]db.Enrollment{}}
}

func (f *fakeEnrollQ) InsertEnrollment(_ context.Context, p db.InsertEnrollmentParams) (db.Enrollment, error) {
	e := db.Enrollment{
		Token:           p.Token,
		Intent:          p.Intent,
		TargetAccountID: p.TargetAccountID,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ExpiresAt:       p.ExpiresAt,
	}
	f.enrollments[p.Token] = e
	return e, nil
}
func (f *fakeEnrollQ) InsertFederatedRegistrationEnrollment(_ context.Context, p db.InsertFederatedRegistrationEnrollmentParams) (db.Enrollment, error) {
	if f.insertErr != nil {
		return db.Enrollment{}, f.insertErr
	}
	f.federatedParams = append(f.federatedParams, p)
	return db.Enrollment{}, nil
}

func (f *fakeEnrollQ) InsertProviderRecoveryEnrollment(_ context.Context, p db.InsertProviderRecoveryEnrollmentParams) (db.Enrollment, error) {
	if f.insertErr != nil {
		return db.Enrollment{}, f.insertErr
	}
	f.providerRecoveryParams = append(f.providerRecoveryParams, p)
	return db.Enrollment{Intent: IntentReset, TargetAccountID: p.TargetAccountID}, nil
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

func TestIssueFederatedRegistration_WritesVerifiedSnapshot(t *testing.T) {
	q := newFakeEnrollQ()
	avatarURL := "https://api.vrchat.cloud/api/1/file/avatar"
	snapshot := FederatedIdentitySnapshot{
		UpstreamIDPID:   41,
		UpstreamIDPSlug: "vrchat-main",
		Issuer:          "https://vrchat.com",
		Subject:         "usr_123",
		DisplayName:     "VRChat User",
		UpstreamData:    []byte(`{"displayName":"VRChat User","userId":"usr_123"}`),
		AvatarURL:       &avatarURL,
	}
	before := time.Now()

	token, expiresAt, err := IssueFederatedRegistration(context.Background(), q, snapshot, 0)
	if err != nil {
		t.Fatalf("IssueFederatedRegistration: %v", err)
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != 32 {
		t.Fatalf("token decodes to %d bytes with error %v, want 32 bytes", len(rawToken), err)
	}
	if delta := expiresAt.Sub(before); delta < FederatedEnrollmentTTL-time.Second || delta > FederatedEnrollmentTTL+time.Second {
		t.Fatalf("expiry delta = %v, want approximately %v", delta, FederatedEnrollmentTTL)
	}
	if len(q.federatedParams) != 1 {
		t.Fatalf("federated inserts = %d, want 1", len(q.federatedParams))
	}
	p := q.federatedParams[0]
	if p.Token != token || !p.ExpiresAt.Valid || !p.ExpiresAt.Time.Equal(expiresAt) {
		t.Fatalf("token/expiry params = %q %+v, want returned values", p.Token, p.ExpiresAt)
	}
	if !p.FederatedUpstreamIdpID.Valid || p.FederatedUpstreamIdpID.Int64 != snapshot.UpstreamIDPID ||
		!p.FederatedUpstreamIdpSlug.Valid || p.FederatedUpstreamIdpSlug.String != snapshot.UpstreamIDPSlug ||
		!p.FederatedUpstreamIss.Valid || p.FederatedUpstreamIss.String != snapshot.Issuer ||
		!p.FederatedUpstreamSub.Valid || p.FederatedUpstreamSub.String != snapshot.Subject ||
		!p.FederatedDisplayName.Valid || p.FederatedDisplayName.String != snapshot.DisplayName ||
		string(p.FederatedUpstreamData) != string(snapshot.UpstreamData) ||
		!p.FederatedAvatarUrl.Valid || p.FederatedAvatarUrl.String != avatarURL {
		t.Fatalf("federated snapshot params = %+v, want %+v", p, snapshot)
	}
}

func TestIssueProviderRecovery_WritesAccountAndProvider(t *testing.T) {
	q := newFakeEnrollQ()
	before := time.Now()

	token, expiresAt, err := IssueProviderRecovery(context.Background(), q, 73, 41, 0)
	if err != nil {
		t.Fatalf("IssueProviderRecovery: %v", err)
	}
	if token == "" {
		t.Fatal("IssueProviderRecovery returned an empty token")
	}
	if delta := expiresAt.Sub(before); delta < FederatedEnrollmentTTL-time.Second || delta > FederatedEnrollmentTTL+time.Second {
		t.Fatalf("expiry delta = %v, want approximately %v", delta, FederatedEnrollmentTTL)
	}
	if len(q.providerRecoveryParams) != 1 {
		t.Fatalf("provider-recovery inserts = %d, want 1", len(q.providerRecoveryParams))
	}
	p := q.providerRecoveryParams[0]
	if p.Token != token || !p.ExpiresAt.Valid || !p.ExpiresAt.Time.Equal(expiresAt) {
		t.Fatalf("token/expiry params = %q %+v, want returned values", p.Token, p.ExpiresAt)
	}
	if !p.TargetAccountID.Valid || p.TargetAccountID.Int32 != 73 {
		t.Fatalf("target account = %+v, want 73", p.TargetAccountID)
	}
	if !p.RecoverySourceUpstreamIdpID.Valid || p.RecoverySourceUpstreamIdpID.Int64 != 41 {
		t.Fatalf("recovery source = %+v, want 41", p.RecoverySourceUpstreamIdpID)
	}
}

func TestIssueFederatedRegistration_RejectsInvalidSnapshot(t *testing.T) {
	valid := FederatedIdentitySnapshot{
		UpstreamIDPID:   41,
		UpstreamIDPSlug: "vrchat-main",
		Issuer:          "https://vrchat.com",
		Subject:         "usr_123",
		DisplayName:     "VRChat User",
		UpstreamData:    []byte(`{"displayName":"VRChat User","userId":"usr_123"}`),
	}
	tests := map[string]func(*FederatedIdentitySnapshot){
		"missing provider ID":   func(s *FederatedIdentitySnapshot) { s.UpstreamIDPID = 0 },
		"missing provider slug": func(s *FederatedIdentitySnapshot) { s.UpstreamIDPSlug = "" },
		"missing issuer":        func(s *FederatedIdentitySnapshot) { s.Issuer = "" },
		"missing subject":       func(s *FederatedIdentitySnapshot) { s.Subject = "" },
		"missing display name":  func(s *FederatedIdentitySnapshot) { s.DisplayName = "" },
		"non-object JSON":       func(s *FederatedIdentitySnapshot) { s.UpstreamData = []byte(`[]`) },
		"non-canonical JSON":    func(s *FederatedIdentitySnapshot) { s.UpstreamData = []byte(`{"userId": "usr_123"}`) },
		"oversized JSON": func(s *FederatedIdentitySnapshot) {
			s.UpstreamData = []byte(`{"data":"` + strings.Repeat("x", 4090) + `"}`)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			q := newFakeEnrollQ()
			snapshot := valid
			snapshot.UpstreamData = bytes.Clone(valid.UpstreamData)
			mutate(&snapshot)

			token, expiresAt, err := IssueFederatedRegistration(context.Background(), q, snapshot, 0)
			if err == nil {
				t.Fatal("IssueFederatedRegistration accepted invalid snapshot")
			}
			if token != "" || !expiresAt.IsZero() {
				t.Fatalf("failure returned token/expiry %q %v", token, expiresAt)
			}
			if len(q.federatedParams) != 0 {
				t.Fatalf("invalid snapshot caused %d inserts", len(q.federatedParams))
			}
		})
	}
}

func TestFederatedEnrollmentIssuance_QueryFailureReturnsNoGrant(t *testing.T) {
	insertErr := errors.New("storage unavailable")
	snapshot := FederatedIdentitySnapshot{
		UpstreamIDPID:   41,
		UpstreamIDPSlug: "vrchat-main",
		Issuer:          "https://vrchat.com",
		Subject:         "usr_123",
		DisplayName:     "VRChat User",
		UpstreamData:    []byte(`{"userId":"usr_123"}`),
	}
	tests := map[string]func(*fakeEnrollQ) (string, time.Time, error){
		"federated registration": func(q *fakeEnrollQ) (string, time.Time, error) {
			return IssueFederatedRegistration(context.Background(), q, snapshot, 0)
		},
		"provider recovery": func(q *fakeEnrollQ) (string, time.Time, error) {
			return IssueProviderRecovery(context.Background(), q, 73, 41, 0)
		},
	}
	for name, issue := range tests {
		t.Run(name, func(t *testing.T) {
			q := newFakeEnrollQ()
			q.insertErr = insertErr
			token, expiresAt, err := issue(q)
			if !errors.Is(err, insertErr) {
				t.Fatalf("error = %v, want wrapped storage error", err)
			}
			if token != "" || !expiresAt.IsZero() {
				t.Fatalf("query failure returned token/expiry %q %v", token, expiresAt)
			}
		})
	}
}
