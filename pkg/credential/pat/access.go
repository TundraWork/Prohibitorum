package pat

import "errors"

// Access is the single authorization level a PAT carries. The levels include
// one another in order: selected_apps < all_apps < full < sudo. No level
// widens the owner's own permissions.
type Access string

const (
	// AccessSelectedApps reaches only the forward-auth applications listed on
	// the token.
	AccessSelectedApps Access = "selected_apps"
	// AccessAllApps reaches every forward-auth application the owner can use.
	AccessAllApps Access = "all_apps"
	// AccessFull adds the management API as the owner; sudo checks fail.
	AccessFull Access = "full"
	// AccessSudo is full access that also passes every sudo check.
	AccessSudo Access = "sudo"
)

// ErrInvalidAccess is returned by ParseAccess for an unknown level.
var ErrInvalidAccess = errors.New("pat: invalid access level")

// ParseAccess matches a level exactly: no case folding or trimming.
func ParseAccess(s string) (Access, error) {
	switch a := Access(s); a {
	case AccessSelectedApps, AccessAllApps, AccessFull, AccessSudo:
		return a, nil
	}
	return "", ErrInvalidAccess
}

// AllowsAPI reports whether the token may call the management API.
func (a Access) AllowsAPI() bool { return a == AccessFull || a == AccessSudo }

// BypassesSudo reports whether sudo checks pass without a fresh step-up.
func (a Access) BypassesSudo() bool { return a == AccessSudo }

// AllowsAllApps reports whether the token reaches every application the owner
// can use rather than a stored list.
func (a Access) AllowsAllApps() bool {
	return a == AccessAllApps || a == AccessFull || a == AccessSudo
}
