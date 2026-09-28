package authn

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"prohibitorum/pkg/weberr"
)

func TestFederationFlowErrorDefinitions(t *testing.T) {
	tests := []struct {
		err       *AuthError
		status    int
		retryable bool
		recovery  string
	}{
		{ErrProviderNotReady(), http.StatusServiceUnavailable, false, ""},
		{ErrVRChatOperatorCredentialsInvalid(), http.StatusUnprocessableEntity, false, ""},
		{ErrVRChatOperatorChallengeInvalid(), http.StatusGone, false, ""},
		{ErrVRChatOperatorCodeInvalid(), http.StatusUnprocessableEntity, true, "retry"},
		{ErrVRChatIdentityInvalid(), http.StatusBadRequest, false, "fix_input"},
		{ErrVRChatProofMissing(), http.StatusConflict, true, "retry"},
		{ErrLocalUsernameRequired(), http.StatusConflict, true, "fix_input"},
		{ErrUpstreamRateLimited(5 * time.Second), http.StatusTooManyRequests, true, "retry"},
		{ErrUpstreamTemporarilyUnavailable(), http.StatusServiceUnavailable, true, "retry"},
		{ErrFederationActionInvalid(), http.StatusConflict, true, "retry"},
	}
	for _, test := range tests {
		t.Run(test.err.Code, func(t *testing.T) {
			definition, ok := weberr.DefinitionFor(test.err.Code)
			if !ok {
				t.Fatal("definition not registered")
			}
			if definition.Status != test.status || definition.Retryable != test.retryable || definition.Recovery != test.recovery {
				t.Fatalf("definition = %#v", definition)
			}
			if test.err.Details != nil {
				t.Fatalf("%s details = %#v", test.err.Code, test.err.Details)
			}
			for key := range definition.DetailKeys {
				if key != "federationName" {
					t.Fatalf("public detail %q allowed for %s", key, test.err.Code)
				}
			}
		})
	}

	named := []struct {
		err    *AuthError
		status int
		extra  string
	}{
		{ErrInviteRequired(), http.StatusForbidden, ""},
		{ErrLinkRequired(), http.StatusForbidden, ""},
		{ErrEmailNotVerified(), http.StatusForbidden, ""},
		{ErrUpstreamError("", ""), http.StatusBadRequest, "upstreamCode"},
		{ErrUpstreamRateLimited(0), http.StatusTooManyRequests, ""},
		{ErrUpstreamTemporarilyUnavailable(), http.StatusServiceUnavailable, ""},
		{ErrProviderNotReady(), http.StatusServiceUnavailable, ""},
		{ErrFederationIdentityConflict(""), http.StatusConflict, ""},
		{ErrFederationInviteProviderMismatch(""), http.StatusConflict, ""},
		{ErrEnrollmentFederationRequired(), http.StatusBadRequest, ""},
	}
	for _, test := range named {
		definition, ok := weberr.DefinitionFor(test.err.Code)
		if !ok || definition.Status != test.status {
			t.Fatalf("definition for %s = %#v", test.err.Code, definition)
		}
		if _, ok := definition.DetailKeys["federationName"]; !ok || len(test.err.Details) != 0 {
			t.Fatalf("%s must allow federationName and omit empty details", test.err.Code)
		}
		want := 1
		if test.extra != "" {
			want = 2
			if _, ok := definition.DetailKeys[test.extra]; !ok {
				t.Fatalf("%s must keep %s", test.err.Code, test.extra)
			}
		}
		if len(definition.DetailKeys) != want {
			t.Fatalf("%s detail keys = %#v", test.err.Code, definition.DetailKeys)
		}
	}

	for _, err := range []*AuthError{ErrFederationIdentityConflict("  Corporate & + 中文  "), ErrFederationInviteProviderMismatch("  Corporate & + 中文  ")} {
		if got := err.Details["federationName"]; got != "Corporate & + 中文" {
			t.Fatalf("%s federationName = %#v", err.Code, got)
		}
	}

	rateLimited := ErrUpstreamRateLimited(5 * time.Second)
	if rateLimited.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %v", rateLimited.RetryAfter)
	}
}

func TestInvalidRoleListsSupportedRoles(t *testing.T) {
	err := ErrInvalidRole()
	allowed, ok := err.Details["allowed"].([]string)
	if !ok {
		t.Fatalf("allowed = %#v, want []string", err.Details["allowed"])
	}
	want := []string{"user", "admin"}
	if len(allowed) != len(want) {
		t.Fatalf("allowed = %#v, want %#v", allowed, want)
	}
	for i := range want {
		if allowed[i] != want[i] {
			t.Fatalf("allowed = %#v, want %#v", allowed, want)
		}
	}
}

func TestInvalidManagerRoleErrorDefinition(t *testing.T) {
	err := ErrInvalidManagerRole()
	definition, ok := weberr.DefinitionFor(err.Code)
	if !ok {
		t.Fatalf("%s is not registered", err.Code)
	}
	if definition.Status != err.Status {
		t.Fatalf("%s status = %d, want %d", err.Code, definition.Status, err.Status)
	}
}

func TestInvitationGroupsUnavailableError(t *testing.T) {
	err := ErrInvitationGroupsUnavailable([]int32{7, 11})
	definition, ok := weberr.DefinitionFor(err.Code)
	if !ok {
		t.Fatal("definition not registered")
	}
	if definition.Status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", definition.Status, http.StatusConflict)
	}
	if _, ok := definition.DetailKeys["groupIds"]; !ok {
		t.Fatal("groupIds is not an allowed public detail")
	}
	groupIDs, ok := err.Details["groupIds"].([]int32)
	if !ok || len(groupIDs) != 2 || groupIDs[0] != 7 || groupIDs[1] != 11 {
		t.Fatalf("groupIds = %#v, want [7 11]", err.Details["groupIds"])
	}
}

func TestWithFederationName(t *testing.T) {
	named := []*AuthError{
		ErrInviteRequired(),
		ErrLinkRequired(),
		ErrEmailNotVerified(),
		ErrUpstreamError("access_denied", "denied by user"),
		ErrUpstreamRateLimited(5 * time.Second),
		ErrUpstreamTemporarilyUnavailable(),
		ErrProviderNotReady(),
		ErrFederationIdentityConflict(""),
		ErrFederationInviteProviderMismatch(""),
		ErrEnrollmentFederationRequired(),
	}
	for _, original := range named {
		t.Run(original.Code, func(t *testing.T) {
			before := len(original.Details)
			got := AsAuthError(WithFederationName(original, "  Corporate & + 中文  "))
			if got == nil || got == original {
				t.Fatalf("want a named copy, got %#v", got)
			}
			if got.Code != original.Code || got.Status != original.Status || got.RetryAfter != original.RetryAfter {
				t.Fatalf("copy changed the error: %#v", got)
			}
			if got.Details["federationName"] != "Corporate & + 中文" {
				t.Fatalf("federationName = %#v", got.Details["federationName"])
			}
			if len(original.Details) != before {
				t.Fatalf("original details changed: %#v", original.Details)
			}
			_, _, details := weberr.Canonicalize(got.Code, got.Details)
			if details["federationName"] != "Corporate & + 中文" {
				t.Fatalf("canonical details = %#v", details)
			}
		})
	}

	upstream := AsAuthError(WithFederationName(ErrUpstreamError("access_denied", ""), "Corporate"))
	if upstream.Details["upstreamCode"] != "access_denied" || upstream.Details["federationName"] != "Corporate" {
		t.Fatalf("upstream_error details = %#v", upstream.Details)
	}
	limited := AsAuthError(WithFederationName(ErrUpstreamRateLimited(5*time.Second), "Corporate"))
	if limited.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %v", limited.RetryAfter)
	}

	alreadyNamed := ErrFederationIdentityConflict("Resolver Name")
	wrapped := fmt.Errorf("wrapped: %w", ErrInviteRequired())
	plain := errors.New("plain")
	unchanged := []struct {
		name string
		err  error
		with string
	}{
		{"unnamed code", ErrFederationStateInvalid(), "Corporate"},
		{"other code", ErrUsernameCollision(), "Corporate"},
		{"empty name", ErrInviteRequired(), ""},
		{"blank name", ErrInviteRequired(), "   "},
		{"already named", alreadyNamed, "Corporate"},
		{"wrapped", wrapped, "Corporate"},
		{"not an AuthError", plain, "Corporate"},
		{"nil", nil, "Corporate"},
	}
	for _, test := range unchanged {
		t.Run(test.name, func(t *testing.T) {
			got := WithFederationName(test.err, test.with)
			if got != test.err {
				t.Fatalf("got %#v, want the original error", got)
			}
			if ae, ok := got.(*AuthError); ok && ae != alreadyNamed && ae.Details["federationName"] != nil {
				t.Fatalf("unexpected federationName: %#v", ae.Details)
			}
		})
	}
	if alreadyNamed.Details["federationName"] != "Resolver Name" {
		t.Fatalf("existing name overwritten: %#v", alreadyNamed.Details)
	}
}
