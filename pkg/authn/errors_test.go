package authn

import (
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
		{ErrFederationIdentityConflict(), http.StatusConflict, false, ""},
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
			if len(definition.DetailKeys) != 0 || test.err.Details != nil {
				t.Fatalf("public details allowed for %s", test.err.Code)
			}
		})
	}

	rateLimited := ErrUpstreamRateLimited(5 * time.Second)
	if rateLimited.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %v", rateLimited.RetryAfter)
	}
}
