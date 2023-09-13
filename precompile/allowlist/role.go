// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import "github.com/ethereum/go-ethereum/common"

// 1. NoRole - this is equivalent to common.Hash{} and deletes the key from the DB when set
// 2. EnabledRole - allowed to call the precompile
// 3. Admin - allowed to both modify the allowlist and call the precompile
// 4. Manager - allowed to add and remove only enabled addresses and also call the precompile. (only after DUpgrade)
var (
	NoRole      = Role(common.BigToHash(common.Big0))
	EnabledRole = Role(common.BigToHash(common.Big1))
	AdminRole   = Role(common.BigToHash(common.Big2))
	ManagerRole = Role(common.BigToHash(common.Big3))
	// Roles should be incremented and not changed.
)

// Enum constants for valid Role
type Role common.Hash

// IsNoRole returns true if [r] indicates no specific role.
func (r Role) IsNoRole() bool {
	switch r {
	case NoRole:
		return true
	default:
		return false
	}
}

// IsAdmin returns true if [r] indicates the permission to modify the allow list.
func (r Role) IsAdmin() bool {
	switch r {
	case AdminRole:
		return true
	default:
		return false
	}
}

// IsEnabled returns true if [r] indicates that it has permission to access the resource.
func (r Role) IsEnabled() bool {
	switch r {
	case AdminRole, EnabledRole, ManagerRole:
		return true
	default:
		return false
	}
}

func (r Role) CanModify(from, target Role) bool {
	switch r {
	case AdminRole:
		return true
	case ManagerRole:
		return (from == EnabledRole || from == NoRole) && (target == EnabledRole || target == NoRole)
	default:
		return false
	}
}

// String returns a string representation of [r].
func (r Role) String() string {
	switch r {
	case NoRole:
		return "NoRole"
	case EnabledRole:
		return "EnabledRole"
	case ManagerRole:
		return "ManagerRole"
	case AdminRole:
		return "AdminRole"
	default:
		return "UnknownRole"
	}
}
