// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import "github.com/ethereum/go-ethereum/common"

// Enum constants for valid Role
type Role common.Hash

// Valid returns true iff [s] represents a valid role.
func (s Role) Valid() bool {
	switch s {
	case NoRole, EnabledRole, AdminRole:
		return true
	default:
		return false
	}
}

// IsNoRole returns true if [s] indicates no specific role.
func (s Role) IsNoRole() bool {
	switch s {
	case NoRole:
		return true
	default:
		return false
	}
}

// IsAdmin returns true if [s] indicates the permission to modify the allow list.
func (s Role) IsAdmin() bool {
	switch s {
	case AdminRole:
		return true
	default:
		return false
	}
}

// IsEnabled returns true if [s] indicates that it has permission to access the resource.
func (s Role) IsEnabled() bool {
	switch s {
	case AdminRole, EnabledRole:
		return true
	default:
		return false
	}
}

// String returns a string representation of [s].
func (s Role) String() string {
	switch s {
	case NoRole:
		return "NoRole"
	case EnabledRole:
		return "EnabledRole"
	case AdminRole:
		return "AdminRole"
	default:
		return "UnknownRole"
	}
}
