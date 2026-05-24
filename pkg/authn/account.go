package authn

import (
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// Permits returns true iff the account is permitted to perform the action
// identified by p. Admins are unconditionally permitted; user-role accounts
// are checked against the matching boolean column.
func Permits(a *db.Account, p contract.Permission) bool {
	if a == nil {
		return false
	}
	if a.Role == "admin" {
		return true
	}
	switch p {
	case contract.PermViewOwnUsage:
		return a.CanViewOwnUsage
	case contract.PermManageOwnAPIKeys:
		return a.CanManageOwnApiKeys
	case contract.PermViewModels:
		return a.CanViewModels
	case contract.PermViewOwnTraces:
		return a.CanViewOwnTraces
	case contract.PermManageOwnProjects:
		return a.CanManageOwnProjects
	}
	return false
}

// PermissionsView projects an account's permission columns into the
// contract.Permissions wire shape. Admin accounts surface as all-true.
func PermissionsView(a *db.Account) contract.Permissions {
	if a == nil {
		return contract.Permissions{}
	}
	if a.Role == "admin" {
		return contract.Permissions{
			ViewOwnUsage:      true,
			ManageOwnAPIKeys:  true,
			ViewModels:        true,
			ViewOwnTraces:     true,
			ManageOwnProjects: true,
		}
	}
	return contract.Permissions{
		ViewOwnUsage:      a.CanViewOwnUsage,
		ManageOwnAPIKeys:  a.CanManageOwnApiKeys,
		ViewModels:        a.CanViewModels,
		ViewOwnTraces:     a.CanViewOwnTraces,
		ManageOwnProjects: a.CanManageOwnProjects,
	}
}
