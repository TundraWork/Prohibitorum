package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/kv"
)

// captureLogrus redirects the global logrus output into a buffer for the
// duration of the test. Must NOT run in parallel with other logrus-emitting
// tests — it mutates the global logger output.
func captureLogrus(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	origOut := logrus.StandardLogger().Out
	logrus.SetOutput(buf)
	return buf, func() { logrus.SetOutput(origOut) }
}

func TestNewFailureWithCausePreservesCauseInPublicProjection(t *testing.T) {
	upstream := errors.New("upstream token endpoint: 401 invalid_client")
	err := NewFailureWithCause(FailureCodeExchange, nil, upstream)

	reason, detail, public, cause, ok := failureProjection(err)
	if !ok || reason != FailureCodeExchange {
		t.Fatalf("projection = (%q, %v), want code_exchange_failed", reason, ok)
	}
	if cause != upstream {
		t.Fatalf("cause = %v, want original upstream error", cause)
	}
	if len(detail) != 0 {
		t.Fatalf("detail = %+v, want empty", detail)
	}
	publicAE := authn.AsAuthError(public)
	if publicAE == nil || publicAE.Code != "federation_state_invalid" {
		t.Fatalf("public error = %v, want federation_state_invalid", public)
	}

	// Unwrap exposes the public error and the cause together, so callers can
	// recover either; the public AuthError is compared by code because each
	// constructor call returns a fresh instance.
	multi, isMulti := err.(interface{ Unwrap() []error })
	if !isMulti {
		t.Fatal("flowFailure lost its Unwrap() []error")
	}
	unwrapped := multi.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("Unwrap() = %v, want public error and cause", unwrapped)
	}
	if authn.AsAuthError(unwrapped[0]) == nil {
		t.Fatalf("Unwrap()[0] = %v, want the public AuthError", unwrapped[0])
	}
	if !errors.Is(err, upstream) || unwrapped[1] != upstream {
		t.Fatal("Unwrap lost the upstream cause")
	}
}

func TestNewFailureWithCauseNilCauseMatchesNewFailure(t *testing.T) {
	bare := NewFailure(FailureSteamVerification, nil)
	withNil := NewFailureWithCause(FailureSteamVerification, nil, nil)

	wantReason, _, _, wantCause, ok := failureProjection(bare)
	if !ok {
		t.Fatal("NewFailure projection failed")
	}
	gotReason, _, _, gotCause, ok := failureProjection(withNil)
	if !ok || gotReason != wantReason {
		t.Fatalf("projection = (%q, %v), want (%q, true)", gotReason, ok, wantReason)
	}
	if gotCause != wantCause {
		t.Fatalf("nil-cause constructor set cause %v, want %v", gotCause, wantCause)
	}

	unknown := NewFailureWithCause(FailureReason("bogus"), nil, errors.New("x"))
	if reason, _, _, _, ok := failureProjection(unknown); !ok || reason != FailureStateInvalid {
		t.Fatalf("unknown reason projected as (%q, %v), want state_invalid", reason, ok)
	}
}

func TestNewRateLimitedFailureWithCauseKeepsRetryAndCause(t *testing.T) {
	upstream := errors.New("429 too many requests")
	err := NewRateLimitedFailureWithCause(90*time.Second, upstream)

	if reason, _, public, cause, ok := failureProjection(err); !ok ||
		reason != FailureUpstreamRateLimited || cause != upstream {
		t.Fatalf("projection = (%q, %v, %v, %v), want upstream_rate_limited with cause", reason, ok, public, cause)
	}
	ae := authn.AsAuthError(err)
	if ae == nil || ae.Code != "upstream_rate_limited" || ae.RetryAfter != 90*time.Second {
		t.Fatalf("public error = %v, want upstream_rate_limited with RetryAfter 90s", ae)
	}

	local := NewRateLimitedFailure(30 * time.Second)
	if _, _, _, cause, ok := failureProjection(local); ok && cause != nil {
		t.Fatalf("local backoff failure carries cause %v, want none", cause)
	}
}

func TestVerifyFlowLogsUpstreamCause(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	service, adapter, _, _ := newServiceHarness(t)
	upstream := errors.New("oidc: token endpoint returned 400 invalid_client")
	adapter.advance = func(json.RawMessage, ActionInput) (AdvanceResult, error) {
		return AdvanceResult{}, NewFailureWithCause(FailureCodeExchange, nil, upstream)
	}
	begin, err := service.BeginPublic(context.Background(), "corp", "/")
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, ProviderSlug: "corp", Protocol: "fake",
		CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	})
	if authn.AsAuthError(err) == nil {
		t.Fatalf("error = %v, want public AuthError", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event=federation_flow_failure") {
		t.Fatalf("log missing federation_flow_failure event:\n%s", out)
	}
	if !strings.Contains(out, "reason=code_exchange_failed") {
		t.Fatalf("log missing allowlisted reason:\n%s", out)
	}
	if !strings.Contains(out, "idp_slug=corp") {
		t.Fatalf("log missing provider slug:\n%s", out)
	}
	if !strings.Contains(out, "flow_id="+begin.FlowID) {
		t.Fatalf("log missing flow id:\n%s", out)
	}
	if !strings.Contains(out, `upstream_error="oidc: token endpoint returned 400 invalid_client"`) {
		t.Fatalf("log missing raw upstream error:\n%s", out)
	}
}

func TestVerifyFlowLocalStateFailureOmitsUpstreamError(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	service, _, _, _ := newServiceHarness(t)

	_, err := service.VerifyFlow(context.Background(), AdvanceRequest{
		FlowID: "missing", ProviderSlug: "corp", Protocol: "fake",
		CallbackRoute: CallbackRoutePublic, Input: ActionInput{Kind: ActionRedirect},
	})
	if authn.AsAuthError(err) == nil {
		t.Fatalf("error = %v, want public AuthError", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event=federation_flow_failure") {
		t.Fatalf("log missing federation_flow_failure event:\n%s", out)
	}
	if strings.Contains(out, "upstream_error") {
		t.Fatalf("local state failure logged an upstream_error field:\n%s", out)
	}
}

func TestVRChatRestoreAfterFailureLogsUpstreamCause(t *testing.T) {
	buf, restore := captureLogrus(t)
	defer restore()

	upstream := errors.New("vrchat api: 404 no such user")
	registry := NewRegistry()
	if err := registry.RegisterDefinition(fakeDefinition{protocol: "vrchat", descriptor: descriptor("vrchat")}); err != nil {
		t.Fatal(err)
	}
	adapter := &serviceFakeAdapter{
		protocol:    "vrchat",
		beginState:  json.RawMessage(`{"step":"proof","proof_token":"private"}`),
		beginAction: NextAction{Kind: ActionPublishProof, Public: map[string]any{"proofUrl": "private-url", "profileUrl": "private-profile"}},
		advance: func(json.RawMessage, ActionInput) (AdvanceResult, error) {
			return AdvanceResult{}, NewFailureWithCause(FailureVRChatIdentityInvalid, nil, upstream)
		},
	}
	if err := registry.RegisterAdapter(adapter); err != nil {
		t.Fatal(err)
	}
	resolver := &serviceFakeResolver{}
	service := NewService(registry, fakeProviderLoader{provider: Provider{ID: 7, Slug: "social", Protocol: "vrchat", Mode: ModeLinkOnly}},
		kv.NewMemoryStore(), resolver, rejectingEnrollmentIssuer(), ServiceConfig{StateTTL: time.Minute, PublicOrigin: "https://login.example.com"})
	begin, err := service.BeginPublic(context.Background(), "social", "/")
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.VerifyFlow(context.Background(), AdvanceRequest{
		FlowID: begin.FlowID, BrowserToken: begin.BrowserToken, CallbackRoute: CallbackRouteLocal,
		Input: ActionInput{Kind: ActionPublishProof},
	})
	if authn.AsAuthError(err) == nil {
		t.Fatalf("error = %v, want public AuthError", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event=federation_flow_failure") {
		t.Fatalf("log missing federation_flow_failure event:\n%s", out)
	}
	if !strings.Contains(out, "reason=vrchat_identity_invalid") {
		t.Fatalf("log missing vrchat reason:\n%s", out)
	}
	if !strings.Contains(out, `upstream_error="vrchat api: 404 no such user"`) {
		t.Fatalf("log missing raw upstream error:\n%s", out)
	}
}
