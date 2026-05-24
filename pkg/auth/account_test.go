package auth

import (
	"bytes"
	"strings"
	"testing"

	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

func TestPermits_AdminAlwaysPasses(t *testing.T) {
	a := &db.Account{Role: "admin"} // all booleans default false
	for _, p := range []contract.Permission{
		contract.PermViewOwnUsage,
		contract.PermManageOwnAPIKeys,
		contract.PermViewModels,
		contract.PermViewOwnTraces,
		contract.PermManageOwnProjects,
	} {
		if !Permits(a, p) {
			t.Errorf("admin should pass %s, did not", p)
		}
	}
}

func TestPermits_UserChecksFields(t *testing.T) {
	a := &db.Account{Role: "user", CanViewOwnUsage: true}
	if !Permits(a, contract.PermViewOwnUsage) {
		t.Error("user with CanViewOwnUsage=true should pass view_own_usage")
	}
	if Permits(a, contract.PermManageOwnAPIKeys) {
		t.Error("user with CanManageOwnApiKeys=false should not pass manage_own_api_keys")
	}
}

func TestPermits_NilAccount(t *testing.T) {
	if Permits(nil, contract.PermViewOwnUsage) {
		t.Error("nil account should not pass")
	}
}

func TestPermits_UnknownPermission(t *testing.T) {
	a := &db.Account{Role: "user"}
	if Permits(a, contract.Permission("nonexistent_perm")) {
		t.Error("unknown permission should not pass for user role")
	}
}

func TestPermissionsView_Admin(t *testing.T) {
	v := PermissionsView(&db.Account{Role: "admin"})
	if !v.ViewOwnUsage || !v.ManageOwnAPIKeys || !v.ViewModels || !v.ViewOwnTraces || !v.ManageOwnProjects {
		t.Errorf("admin should be all-true, got %+v", v)
	}
}

func TestPermissionsView_User(t *testing.T) {
	v := PermissionsView(&db.Account{Role: "user", CanViewModels: true, CanViewOwnUsage: true})
	if !v.ViewModels || !v.ViewOwnUsage {
		t.Errorf("set fields should be true, got %+v", v)
	}
	if v.ManageOwnAPIKeys || v.ViewOwnTraces {
		t.Errorf("unset fields should be false, got %+v", v)
	}
}

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
	err := ErrNoSession()
	if AsAuthError(err) == nil {
		t.Fatal("direct AuthError should be detected")
	}
	if AsAuthError(nil) != nil {
		t.Error("nil should return nil")
	}
}

func TestAuthErrorString(t *testing.T) {
	e := ErrLastAdmin()
	s := e.Error()
	if !strings.Contains(s, "last_admin") {
		t.Errorf("error string should contain code: %q", s)
	}
}
