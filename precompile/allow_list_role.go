// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import "github.com/ethereum/go-ethereum/common"

// Enum constants for valid AllowListRole
type AllowListRole common.Hash

// Valid returns true iff [s] represents a valid role.
func (s AllowListRole) Valid() bool {
	switch s {
	case AllowListNoRole, AllowListEnabled, AllowListAdmin:
		return true
	default:
		return false
	}
}

// IsNoRole returns true if [s] indicates no specific role.
func (s AllowListRole) IsNoRole() bool {
	switch s {
	case AllowListNoRole:
		return true
	default:
		return false
	}
}

// IsAdmin returns true if [s] indicates the permission to modify the allow list.
func (s AllowListRole) IsAdmin() bool {
	switch s {
	case AllowListAdmin:
		return true
	default:
		return false
	}
}

// IsEnabled returns true if [s] indicates that it has permission to access the resource.
func (s AllowListRole) IsEnabled() bool {
	switch s {
	case AllowListAdmin, AllowListEnabled:
		return true
	default:
		return false
	}
}
