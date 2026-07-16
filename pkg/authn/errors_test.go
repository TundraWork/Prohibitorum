package authn

import (
	"net/http"
	"testing"
	"time"

	"prohibitorum/pkg/weberr"
)

func TestVRChatOperatorErrorDefinitions(t *testing.T) {
	tests := []struct {
		err       *AuthError
		status    int
		retryable bool
	}{
		{ErrVRChatOperatorCredentialsInvalid(), http.StatusUnprocessableEntity, false},
		{ErrVRChatOperatorChallengeInvalid(), http.StatusGone, false},
		{ErrVRChatOperatorCodeInvalid(), http.StatusUnprocessableEntity, true},
		{ErrUpstreamRateLimited(5 * time.Second), http.StatusTooManyRequests, true},
		{ErrUpstreamTemporarilyUnavailable(), http.StatusServiceUnavailable, true},
		{ErrVRChatIdentityInvalid(), http.StatusUnprocessableEntity, false},
		{ErrVRChatProofMissing(), http.StatusUnprocessableEntity, false},
	}
	for _, test := range tests {
		t.Run(test.err.Code, func(t *testing.T) {
			definition, ok := weberr.DefinitionFor(test.err.Code)
			if !ok {
				t.Fatal("definition not registered")
			}
			if definition.Status != test.status || definition.Retryable != test.retryable {
				t.Fatalf("definition = %#v", definition)
			}
			if len(definition.DetailKeys) != 0 || test.err.Details != nil {
				t.Fatalf("public details allowed for %s", test.err.Code)
			}
		})
	}
}
