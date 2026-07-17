package vrchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

type proofClientStub struct {
	calls      int
	user       PublicUser
	err        error
	publicUser func() (PublicUser, error)
	decodeErr  error
	unusable   bool
}

func (c *proofClientStub) PublicUser(_ context.Context, _ string, _ []http.Cookie) (PublicUser, []http.Cookie, error) {
	c.calls++
	if c.publicUser != nil {
		user, err := c.publicUser()
		return user, nil, err
	}
	return c.user, nil, c.err
}
func (c *proofClientStub) DecodeCookies(_ []byte, now time.Time) ([]http.Cookie, error) {
	if c.decodeErr != nil {
		return nil, c.decodeErr
	}
	if c.unusable {
		return nil, nil
	}
	return []http.Cookie{{Name: "auth", Value: "cookie", Path: "/", Secure: true, HttpOnly: true, Expires: now.Add(time.Hour)}}, nil
}

type proofSecretsStub struct{ plaintext []byte }

func (s *proofSecretsStub) OpenProviderSecret(_ federation.SealedSecret, _ int64) ([]byte, error) {
	return s.plaintext, nil
}

type proofQueriesStub struct {
	calls int
	got   db.InvalidateVRChatOperatorSecretParams
	err   error
}

func (q *proofQueriesStub) InvalidateVRChatOperatorSecret(_ context.Context, params db.InvalidateVRChatOperatorSecretParams) (db.UpstreamIdp, error) {
	q.calls++
	q.got = params
	return db.UpstreamIdp{}, q.err
}

type proofAuditStub struct{ records []audit.Record }

func (w *proofAuditStub) Record(_ context.Context, record audit.Record) error {
	w.records = append(w.records, record)
	return nil
}

func readyVRChatProvider() federation.Provider {
	now := time.Now().UTC()
	return federation.Provider{ID: 7, Slug: "vrchat", Protocol: Protocol, Mode: federation.ModeAutoProvision, Config: json.RawMessage(`{}`), Secret: &federation.SealedSecret{Ciphertext: []byte("cipher"), Nonce: []byte("nonce"), KeyVersion: 3}, SecretStatus: "valid", SecretValidatedAt: &now}
}

func TestVRChatAdapterBeginAndCollectIdentity(t *testing.T) {
	adapter := NewAdapter(&proofClientStub{}, &proofSecretsStub{}, kv.NewMemoryStore(), &proofQueriesStub{}, "https://login.example.com", nil)
	adapter.random = bytes.NewReader(bytes.Repeat([]byte{0x2a}, 32))
	state, action, err := adapter.Begin(context.Background(), readyVRChatProvider(), federation.BeginContext{Intent: federation.IntentLogin})
	if err != nil || action.Kind != federation.ActionCollectIdentity || action.Public != nil {
		t.Fatalf("Begin = %s, %+v, %v", state, action, err)
	}
	result, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionCollectIdentity, Identity: testUserID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Next == nil || result.Next.Kind != federation.ActionPublishProof {
		t.Fatalf("Next = %+v", result.Next)
	}
	wantProfile := profileURLBase + testUserID
	if result.Next.Public["profileUrl"] != wantProfile {
		t.Errorf("profileUrl = %v", result.Next.Public["profileUrl"])
	}
	wantToken := "KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio"
	if result.Next.Public["proofUrl"] != "https://login.example.com/verify/vrchat/"+wantToken {
		t.Errorf("proofUrl = %v", result.Next.Public["proofUrl"])
	}
	if result.Candidate == nil || result.Candidate.Issuer != issuerURL || result.Candidate.Subject != testUserID {
		t.Errorf("Candidate = %+v", result.Candidate)
	}
	var private map[string]any
	if err := json.Unmarshal(result.State, &private); err != nil {
		t.Fatal(err)
	}
	if len(private) != 3 || private["step"] != "proof" || private["user_id"] != testUserID || private["proof_token"] != wantToken {
		t.Errorf("private state = %#v", private)
	}
}

func TestVRChatAdapterPublishProofExactIdentity(t *testing.T) {
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	client := &proofClientStub{user: PublicUser{ID: testUserID, DisplayName: "Display", BioLinks: []string{"https://LOGIN.EXAMPLE.COM:443/verify/vrchat/" + token}, CurrentAvatarThumbnailImageURL: "https://api.vrchat.cloud/avatar.png"}}
	plaintext := []byte("serialized cookies")
	adapter := NewAdapter(client, &proofSecretsStub{plaintext: plaintext}, kv.NewMemoryStore(), &proofQueriesStub{}, "https://login.example.com", nil)
	state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: token})
	result, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 {
		t.Fatalf("PublicUser calls = %d", client.calls)
	}
	identity := result.Identity
	if identity == nil || identity.Issuer != issuerURL || identity.Subject != testUserID || identity.Username != "" || identity.DisplayName != "Display" || identity.AvatarURL != "https://api.vrchat.cloud/avatar.png" {
		t.Fatalf("Identity = %+v", identity)
	}
	if len(identity.AMR) != 1 || identity.AMR[0] != "vrchat_profile" {
		t.Errorf("AMR = %#v", identity.AMR)
	}
	wantUpstream := map[string]string{"userId": testUserID, "displayName": "Display", "profileUrl": profileURLBase + testUserID}
	if len(identity.UpstreamData) != len(wantUpstream) {
		t.Fatalf("UpstreamData = %#v", identity.UpstreamData)
	}
	for key, want := range wantUpstream {
		if identity.UpstreamData[key] != want {
			t.Errorf("UpstreamData[%s] = %q", key, identity.UpstreamData[key])
		}
	}
	for i, value := range plaintext {
		if value != 0 {
			t.Fatalf("plaintext byte %d not cleared", i)
		}
	}
}

func TestVRChatAdapterRateLimitCreatesSharedProviderBackoff(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := kv.NewMemoryStore()
	firstClient := &proofClientStub{err: &HTTPError{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Minute, Category: "rate_limited"}}
	adapter := NewAdapter(firstClient, &proofSecretsStub{plaintext: []byte("cookies")}, store, &proofQueriesStub{}, "https://login.example.com", nil)
	adapter.now = func() time.Time { return now }
	state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	_, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
	var publicErr *authn.AuthError
	if !errors.As(err, &publicErr) || publicErr.Code != "upstream_rate_limited" || publicErr.RetryAfter != 15*time.Minute {
		t.Fatalf("first error = %#v (%v)", publicErr, err)
	}
	secondClient := &proofClientStub{}
	second := NewAdapter(secondClient, &proofSecretsStub{plaintext: []byte("cookies")}, store, &proofQueriesStub{}, "https://login.example.com", nil)
	second.now = func() time.Time { return now.Add(time.Minute) }
	_, err = second.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
	if !errors.As(err, &publicErr) || publicErr.RetryAfter != 14*time.Minute {
		t.Fatalf("shared backoff error = %#v (%v)", publicErr, err)
	}
	if secondClient.calls != 0 {
		t.Fatalf("PublicUser called %d times during shared backoff", secondClient.calls)
	}
}

func TestVRChatAdapterConcurrentRateLimitsNeverShortenSharedDeadline(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store := kv.NewMemoryStore()
	state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	input := federation.ActionInput{Kind: federation.ActionPublishProof}
	entered := make(chan struct{})
	release := make(chan struct{})
	shortClient := &proofClientStub{publicUser: func() (PublicUser, error) {
		close(entered)
		<-release
		return PublicUser{}, &HTTPError{Status: http.StatusTooManyRequests, RetryAfter: time.Minute, Category: "rate_limited"}
	}}
	short := NewAdapter(shortClient, &proofSecretsStub{plaintext: []byte("short")}, store, &proofQueriesStub{}, "https://login.example.com", nil)
	short.now = func() time.Time { return now }
	shortResult := make(chan error, 1)
	go func() {
		_, err := short.Advance(context.Background(), readyVRChatProvider(), state, input)
		shortResult <- err
	}()
	<-entered
	longClient := &proofClientStub{err: &HTTPError{Status: http.StatusTooManyRequests, RetryAfter: maxProviderBackoff, Category: "rate_limited"}}
	long := NewAdapter(longClient, &proofSecretsStub{plaintext: []byte("long")}, store, &proofQueriesStub{}, "https://login.example.com", nil)
	long.now = func() time.Time { return now }
	if _, err := long.Advance(context.Background(), readyVRChatProvider(), state, input); err == nil {
		t.Fatal("long rate limit unexpectedly succeeded")
	}
	close(release)
	if err := <-shortResult; err == nil {
		t.Fatal("short rate limit unexpectedly succeeded")
	}
	raw, err := store.Get(context.Background(), providerBackoffKey(7))
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(maxProviderBackoff); !deadline.Equal(want) {
		t.Fatalf("shared deadline = %s, want %s", deadline, want)
	}
}

func TestVRChatAdapterAuthenticationFailureInvalidatesOnlySnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		queryErr  error
		wantAudit bool
	}{
		{name: "unauthorized transitioned", status: http.StatusUnauthorized, wantAudit: true},
		{name: "forbidden transitioned", status: http.StatusForbidden, wantAudit: true},
		{name: "concurrent replacement", status: http.StatusUnauthorized, queryErr: pgx.ErrNoRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := &proofQueriesStub{err: test.queryErr}
			client := &proofClientStub{err: &HTTPError{Status: test.status, Category: "authentication"}}
			writer := &proofAuditStub{}
			adapter := NewAdapter(client, &proofSecretsStub{plaintext: []byte("cookies")}, kv.NewMemoryStore(), queries, "https://login.example.com", writer)
			state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
			_, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
			var publicErr *authn.AuthError
			if !errors.As(err, &publicErr) || publicErr.Code != "provider_not_ready" {
				t.Fatalf("error = %#v (%v)", publicErr, err)
			}
			if queries.calls != 1 || queries.got.ID != 7 || queries.got.Slug != "vrchat" ||
				!bytes.Equal(queries.got.ExpectedSecretEnc, []byte("cipher")) ||
				!bytes.Equal(queries.got.ExpectedSecretNonce, []byte("nonce")) ||
				!queries.got.ExpectedKeyVersion.Valid || queries.got.ExpectedKeyVersion.Int32 != 3 {
				t.Fatalf("invalidation params = %+v, calls=%d", queries.got, queries.calls)
			}
			if got := len(writer.records); got != btoi(test.wantAudit) {
				t.Fatalf("audit records = %#v", writer.records)
			}
			if test.wantAudit {
				record := writer.records[0]
				if record.Event != "vrchat_operator_session_invalidated" || len(record.Detail) != 3 ||
					record.Detail["slug"] != "vrchat" || record.Detail["action"] != "proof_lookup" ||
					record.Detail["category"] != "authentication" {
					t.Fatalf("audit record = %#v", record)
				}
			}
		})
	}
}

func TestVRChatAdapterInvalidCookieSnapshotInvalidatesOnlyMatchingSecret(t *testing.T) {
	for _, test := range []struct {
		name      string
		unusable  bool
		queryErr  error
		wantAudit bool
	}{
		{name: "malformed transitioned", wantAudit: true},
		{name: "unusable transitioned", unusable: true, wantAudit: true},
		{name: "malformed stale replacement", queryErr: pgx.ErrNoRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := &proofQueriesStub{err: test.queryErr}
			writer := &proofAuditStub{}
			client := &proofClientStub{decodeErr: errors.New("malformed cookies"), unusable: test.unusable}
			if test.unusable {
				client.decodeErr = nil
			}
			adapter := NewAdapter(client, &proofSecretsStub{plaintext: []byte("cookies")}, kv.NewMemoryStore(), queries, "https://login.example.com", writer)
			state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
			_, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
			var publicErr *authn.AuthError
			if !errors.As(err, &publicErr) || publicErr.Code != "provider_not_ready" {
				t.Fatalf("error = %#v (%v)", publicErr, err)
			}
			if queries.calls != 1 {
				t.Fatalf("invalidation calls = %d", queries.calls)
			}
			if got := len(writer.records); got != btoi(test.wantAudit) {
				t.Fatalf("audit records = %#v", writer.records)
			}
		})
	}
}

func TestVRChatAdapterPublicUserNotFoundIsInvalidIdentity(t *testing.T) {
	client := &proofClientStub{err: &HTTPError{Status: http.StatusNotFound, Category: "unexpected_status"}}
	adapter := NewAdapter(client, &proofSecretsStub{plaintext: []byte("cookies")}, kv.NewMemoryStore(), &proofQueriesStub{}, "https://login.example.com", nil)
	state, _ := json.Marshal(adapterState{Step: stepProof, UserID: testUserID, ProofToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	_, err := adapter.Advance(context.Background(), readyVRChatProvider(), state, federation.ActionInput{Kind: federation.ActionPublishProof})
	if reason, ok := federation.FailureReasonOf(err); !ok || reason != federation.FailureVRChatIdentityInvalid {
		t.Fatalf("error = %v, reason = %q", err, reason)
	}
}

func TestVRChatAdapterRejectsNonCanonicalPrivateState(t *testing.T) {
	adapter := NewAdapter(&proofClientStub{}, &proofSecretsStub{}, kv.NewMemoryStore(), &proofQueriesStub{}, "https://login.example.com", nil)
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"step":"identify","unexpected":true}`),
		json.RawMessage(`{"step":"proof","user_id":"` + testUserID + `","proof_token":"short"}`),
		json.RawMessage(`{"step":"identify"} {}`),
	} {
		_, err := adapter.Advance(context.Background(), readyVRChatProvider(), raw, federation.ActionInput{Kind: federation.ActionPublishProof})
		if reason, ok := federation.FailureReasonOf(err); !ok || reason != federation.FailureStateInvalid {
			t.Errorf("state %s error = %v, reason=%q", raw, err, reason)
		}
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}
