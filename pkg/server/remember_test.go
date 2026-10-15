package server

import (
	"net/http/httptest"
	"testing"
)

func TestRememberRequestedOnlyLiteralTrue(t *testing.T) {
	for query, want := range map[string]bool{
		"":                             false,
		"?remember=true":               true,
		"?remember=1":                  false,
		"?remember=TRUE":               false,
		"?remember=false":              false,
		"?return_to=%2F&remember=true": true,
	} {
		r := httptest.NewRequest("POST", "/api/prohibitorum/auth/login/complete"+query, nil)
		if got := rememberRequested(r); got != want {
			t.Errorf("rememberRequested(%q) = %v, want %v", query, got, want)
		}
	}
}
