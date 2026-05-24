package account

import (
	"bytes"
	"strings"
	"testing"

	"prohibitorum/pkg/authn"
)

func TestValidateUsername_Valid(t *testing.T) {
	valid := []string{"al", "alice", "bob_smith", "user-1", "ab", "a_-9", "abcdefghij0123456789-_abcdefghij"}
	for _, s := range valid {
		if err := ValidateUsername(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
}

func TestValidateUsername_Invalid(t *testing.T) {
	invalid := []string{
		"",        // empty
		"a",       // too short
		"Alice",   // uppercase
		"alice@",  // illegal char
		" alice",  // leading space
		"alice ",  // trailing space
		"alice.b", // dot illegal
		strings.Repeat("a", 33), // too long
	}
	for _, s := range invalid {
		if err := ValidateUsername(s); err == nil {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestValidateDisplayName(t *testing.T) {
	if err := ValidateDisplayName("Alice Smith"); err != nil {
		t.Errorf("normal display name should pass: %v", err)
	}
	if err := ValidateDisplayName(strings.Repeat("x", 128)); err != nil {
		t.Errorf("128 chars should pass: %v", err)
	}
	if err := ValidateDisplayName(strings.Repeat("x", 129)); err == nil {
		t.Error("129 chars should fail")
	}
	if err := ValidateDisplayName(""); err == nil {
		t.Error("empty should fail")
	}
	if err := ValidateDisplayName("hello\nworld"); err == nil {
		t.Error("newline should fail (control char)")
	}
	if err := ValidateDisplayName("hello\x7fworld"); err == nil {
		t.Error("0x7f should fail (control char)")
	}
}

func TestGenerateUserHandle(t *testing.T) {
	a, err := GenerateUserHandle()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Errorf("want 64 bytes, got %d", len(a))
	}
	b, err := GenerateUserHandle()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("two consecutive generations should differ")
	}
}

func TestAsAuthError(t *testing.T) {
	err := authn.ErrNoSession()
	if authn.AsAuthError(err) == nil {
		t.Fatal("direct AuthError should be detected")
	}
	if authn.AsAuthError(nil) != nil {
		t.Error("nil should return nil")
	}
}

func TestAuthErrorString(t *testing.T) {
	e := authn.ErrLastAdmin()
	s := e.Error()
	if !strings.Contains(s, "last_admin") {
		t.Errorf("error string should contain code: %q", s)
	}
}
