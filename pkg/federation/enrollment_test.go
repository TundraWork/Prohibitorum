package federation_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	federation "prohibitorum/pkg/federation"
)

type fakeEnrollmentQueries struct {
	identity           db.AccountIdentity
	identityErr        error
	identityLookups    []db.GetAccountIdentityByIssuerSubParams
	account            db.Account
	accountErr         error
	accountIDs         []int32
	federatedParams    []db.InsertFederatedRegistrationEnrollmentParams
	recoveryParams     []db.InsertProviderRecoveryEnrollmentParams
	federatedInsertErr error
	recoveryInsertErr  error
}

func (f *fakeEnrollmentQueries) GetAccountIdentityByIssuerSub(_ context.Context, p db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error) {
	f.identityLookups = append(f.identityLookups, p)
	return f.identity, f.identityErr
}

func (f *fakeEnrollmentQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	f.accountIDs = append(f.accountIDs, id)
	return f.account, f.accountErr
}

func (f *fakeEnrollmentQueries) InsertFederatedRegistrationEnrollment(_ context.Context, p db.InsertFederatedRegistrationEnrollmentParams) (db.Enrollment, error) {
	if f.federatedInsertErr != nil {
		return db.Enrollment{}, f.federatedInsertErr
	}
	f.federatedParams = append(f.federatedParams, p)
	return db.Enrollment{}, nil
}

func (f *fakeEnrollmentQueries) InsertProviderRecoveryEnrollment(_ context.Context, p db.InsertProviderRecoveryEnrollmentParams) (db.Enrollment, error) {
	if f.recoveryInsertErr != nil {
		return db.Enrollment{}, f.recoveryInsertErr
	}
	f.recoveryParams = append(f.recoveryParams, p)
	return db.Enrollment{}, nil
}

type enrollmentAuditRecorder struct {
	records []audit.Record
}

func (r *enrollmentAuditRecorder) Record(_ context.Context, record audit.Record) error {
	r.records = append(r.records, record)
	return nil
}

func vrchatEnrollmentProvider() federation.Provider {
	return federation.Provider{ID: 41, Slug: "vrchat-main", Protocol: "vrchat", Mode: federation.ModeLinkOnly}
}

func vrchatVerifiedIdentity() federation.VerifiedIdentity {
	return federation.VerifiedIdentity{
		Issuer:       "https://vrchat.com",
		Subject:      "usr_123",
		Username:     "must-not-be-persisted",
		DisplayName:  "VRChat User",
		AvatarURL:    "https://api.vrchat.cloud/api/1/file/avatar",
		UpstreamData: map[string]string{"userId": "usr_123", "displayName": "VRChat User"},
	}
}

func TestVRChatEnrollmentIssuer_UnknownIdentityIssuesFederatedRegistration(t *testing.T) {
	q := &fakeEnrollmentQueries{identityErr: pgx.ErrNoRows}
	a := &enrollmentAuditRecorder{}
	issuer := federation.NewVRChatEnrollmentIssuer(q, a)
	before := time.Now()

	grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if grant.Token == "" || grant.Intent != "federated_register" {
		t.Fatalf("grant = %+v, want federated_register with token", grant)
	}
	if delta := grant.ExpiresAt.Sub(before); delta < 15*time.Minute-time.Second || delta > 15*time.Minute+time.Second {
		t.Fatalf("expiry delta = %v, want approximately 15m", delta)
	}
	if len(q.federatedParams) != 1 || len(q.recoveryParams) != 0 {
		t.Fatalf("insert calls: federated=%d recovery=%d, want 1/0", len(q.federatedParams), len(q.recoveryParams))
	}
	p := q.federatedParams[0]
	if !p.FederatedUpstreamIdpID.Valid || p.FederatedUpstreamIdpID.Int64 != 41 ||
		!p.FederatedUpstreamIdpSlug.Valid || p.FederatedUpstreamIdpSlug.String != "vrchat-main" ||
		!p.FederatedUpstreamIss.Valid || p.FederatedUpstreamIss.String != "https://vrchat.com" ||
		!p.FederatedUpstreamSub.Valid || p.FederatedUpstreamSub.String != "usr_123" ||
		!p.FederatedDisplayName.Valid || p.FederatedDisplayName.String != "VRChat User" ||
		string(p.FederatedUpstreamData) != `{"displayName":"VRChat User","userId":"usr_123"}` ||
		!p.FederatedAvatarUrl.Valid || p.FederatedAvatarUrl.String != "https://api.vrchat.cloud/api/1/file/avatar" {
		t.Fatalf("snapshot insert = %+v", p)
	}
}

func TestVRChatEnrollmentIssuer_ExistingIdentityIssuesProviderRecovery(t *testing.T) {
	q := &fakeEnrollmentQueries{
		identity: db.AccountIdentity{AccountID: 73, UpstreamIdpID: 41},
		account:  db.Account{ID: 73},
	}
	issuer := federation.NewVRChatEnrollmentIssuer(q, &enrollmentAuditRecorder{})

	grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if grant.Token == "" || grant.Intent != "reset" {
		t.Fatalf("grant = %+v, want reset with token", grant)
	}
	if len(q.accountIDs) != 1 || q.accountIDs[0] != 73 {
		t.Fatalf("account lookups = %+v, want [73]", q.accountIDs)
	}
	if len(q.recoveryParams) != 1 || len(q.federatedParams) != 0 {
		t.Fatalf("insert calls: recovery=%d federated=%d, want 1/0", len(q.recoveryParams), len(q.federatedParams))
	}
	p := q.recoveryParams[0]
	if !p.TargetAccountID.Valid || p.TargetAccountID.Int32 != 73 ||
		!p.RecoverySourceUpstreamIdpID.Valid || p.RecoverySourceUpstreamIdpID.Int64 != 41 {
		t.Fatalf("provider recovery params = %+v", p)
	}
}

func TestVRChatEnrollmentIssuer_RejectsInvalidProviderBindingBeforeLookup(t *testing.T) {
	tests := map[string]federation.Provider{
		"wrong protocol": {ID: 41, Slug: "oidc-main", Protocol: "oidc", Mode: federation.ModeLinkOnly},
		"wrong mode":     {ID: 41, Slug: "vrchat-main", Protocol: "vrchat", Mode: federation.ModeAutoProvision},
	}
	for name, provider := range tests {
		t.Run(name, func(t *testing.T) {
			q := &fakeEnrollmentQueries{
				identity: db.AccountIdentity{AccountID: 73, UpstreamIdpID: 41},
				account:  db.Account{ID: 73},
			}
			issuer := federation.NewVRChatEnrollmentIssuer(q, &enrollmentAuditRecorder{})

			grant, err := issuer.Issue(context.Background(), provider, vrchatVerifiedIdentity())
			if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
				t.Fatalf("error = %v, want federation_state_invalid", err)
			}
			if grant != (federation.EnrollmentGrant{}) {
				t.Fatalf("failure returned grant %+v", grant)
			}
			if len(q.identityLookups) != 0 || len(q.accountIDs) != 0 ||
				len(q.federatedParams) != 0 || len(q.recoveryParams) != 0 {
				t.Fatalf("invalid provider touched storage: identity=%d account=%d federated=%d recovery=%d",
					len(q.identityLookups), len(q.accountIDs), len(q.federatedParams), len(q.recoveryParams))
			}
		})
	}
}

func TestVRChatEnrollmentIssuer_RejectsIdentityBoundToAnotherProvider(t *testing.T) {
	q := &fakeEnrollmentQueries{
		identity: db.AccountIdentity{AccountID: 73, UpstreamIdpID: 99},
		account:  db.Account{ID: 73},
	}
	a := &enrollmentAuditRecorder{}
	issuer := federation.NewVRChatEnrollmentIssuer(q, a)

	grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "federation_state_invalid" {
		t.Fatalf("error = %v, want federation_state_invalid", err)
	}
	if grant != (federation.EnrollmentGrant{}) {
		t.Fatalf("failure returned grant %+v", grant)
	}
	if len(q.accountIDs) != 0 || len(q.federatedParams) != 0 || len(q.recoveryParams) != 0 {
		t.Fatalf("provider mismatch touched account/enrollment storage: account=%d federated=%d recovery=%d",
			len(q.accountIDs), len(q.federatedParams), len(q.recoveryParams))
	}
	if len(a.records) != 0 {
		t.Fatalf("provider mismatch emitted audit records: %+v", a.records)
	}
}

func TestVRChatEnrollmentIssuer_DisabledAccountReturnsOpaqueCredentialsFailure(t *testing.T) {
	q := &fakeEnrollmentQueries{
		identity: db.AccountIdentity{AccountID: 73, UpstreamIdpID: 41},
		account: db.Account{
			ID:          73,
			Username:    "private-username",
			DisplayName: "Private Name",
			Disabled:    true,
		},
	}
	a := &enrollmentAuditRecorder{}
	issuer := federation.NewVRChatEnrollmentIssuer(q, a)

	grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
	if ae := authn.AsAuthError(err); ae == nil || ae.Code != "bad_credentials" {
		t.Fatalf("error = %v, want bad_credentials", err)
	}
	if grant != (federation.EnrollmentGrant{}) {
		t.Fatalf("failure returned grant %+v", grant)
	}
	if len(q.federatedParams) != 0 || len(q.recoveryParams) != 0 {
		t.Fatalf("disabled account caused enrollment insert: federated=%d recovery=%d",
			len(q.federatedParams), len(q.recoveryParams))
	}
	if len(a.records) != 0 {
		t.Fatalf("disabled account emitted audit records containing lookup data: %+v", a.records)
	}
}

func TestVRChatEnrollmentIssuer_SuccessAuditContainsOnlySafeSourceDetail(t *testing.T) {
	tests := map[string]struct {
		q          *fakeEnrollmentQueries
		wantIntent string
	}{
		"registration": {
			q:          &fakeEnrollmentQueries{identityErr: pgx.ErrNoRows},
			wantIntent: "federated_register",
		},
		"recovery": {
			q: &fakeEnrollmentQueries{
				identity: db.AccountIdentity{AccountID: 73, UpstreamIdpID: 41},
				account:  db.Account{ID: 73, Username: "private-username", DisplayName: "Private Name"},
			},
			wantIntent: "reset",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a := &enrollmentAuditRecorder{}
			issuer := federation.NewVRChatEnrollmentIssuer(tc.q, a)

			grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			if len(a.records) != 1 {
				t.Fatalf("audit records = %d, want 1", len(a.records))
			}
			record := a.records[0]
			if record.Factor != audit.FactorEnrollment || record.Event != audit.EventEnrollmentIssued {
				t.Fatalf("audit factor/event = %q/%q", record.Factor, record.Event)
			}
			if record.AccountID != nil || record.CredentialRef != nil || record.UserAgent != "" {
				t.Fatalf("audit exposed account/credential/profile fields: %+v", record)
			}
			wantDetail := map[string]any{
				"intent":       tc.wantIntent,
				"idp_slug":     "vrchat-main",
				"upstream_iss": "https://vrchat.com",
				"upstream_sub": "usr_123",
			}
			if !reflect.DeepEqual(record.Detail, wantDetail) {
				t.Fatalf("audit detail = %#v, want only %#v (grant token %q must not appear)", record.Detail, wantDetail, grant.Token)
			}
		})
	}
}

func TestVRChatEnrollmentIssuer_RejectsOversizedMetadataBeforeStorage(t *testing.T) {
	q := &fakeEnrollmentQueries{identityErr: pgx.ErrNoRows}
	issuer := federation.NewVRChatEnrollmentIssuer(q, &enrollmentAuditRecorder{})
	identity := vrchatVerifiedIdentity()
	identity.UpstreamData = map[string]string{"oversized": strings.Repeat("x", 1025)}

	grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), identity)
	if err == nil {
		t.Fatal("Issue accepted oversized upstream metadata")
	}
	if grant != (federation.EnrollmentGrant{}) {
		t.Fatalf("failure returned grant %+v", grant)
	}
	if len(q.identityLookups) != 0 || len(q.federatedParams) != 0 || len(q.recoveryParams) != 0 {
		t.Fatalf("oversized metadata touched storage: identity=%d federated=%d recovery=%d",
			len(q.identityLookups), len(q.federatedParams), len(q.recoveryParams))
	}
}

func TestVRChatEnrollmentIssuer_InsertFailureReturnsNoGrantOrAudit(t *testing.T) {
	insertErr := errors.New("storage unavailable")
	tests := map[string]*fakeEnrollmentQueries{
		"registration": {
			identityErr:        pgx.ErrNoRows,
			federatedInsertErr: insertErr,
		},
		"recovery": {
			identity:          db.AccountIdentity{AccountID: 73, UpstreamIdpID: 41},
			account:           db.Account{ID: 73},
			recoveryInsertErr: insertErr,
		},
	}
	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			a := &enrollmentAuditRecorder{}
			issuer := federation.NewVRChatEnrollmentIssuer(q, a)

			grant, err := issuer.Issue(context.Background(), vrchatEnrollmentProvider(), vrchatVerifiedIdentity())
			if !errors.Is(err, insertErr) {
				t.Fatalf("error = %v, want wrapped storage error", err)
			}
			if grant != (federation.EnrollmentGrant{}) {
				t.Fatalf("storage failure returned grant %+v", grant)
			}
			if len(a.records) != 0 {
				t.Fatalf("failed insert emitted audit records: %+v", a.records)
			}
		})
	}
}
