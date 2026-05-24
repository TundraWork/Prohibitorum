package authn

import (
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
