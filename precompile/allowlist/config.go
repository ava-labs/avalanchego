// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"fmt"

	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ethereum/go-ethereum/common"
)

// AllowListConfig specifies the initial set of addresses with Admin or Enabled roles.
type AllowListConfig struct {
	AdminAddresses   []common.Address `json:"adminAddresses,omitempty"`   // initial admin addresses
	EnabledAddresses []common.Address `json:"enabledAddresses,omitempty"` // initial enabled addresses
}

// Configure initializes the address space of [precompileAddr] by initializing the role of each of
// the addresses in [AllowListAdmins].
func (c *AllowListConfig) Configure(state contract.StateDB, precompileAddr common.Address) error {
	for _, enabledAddr := range c.EnabledAddresses {
		SetAllowListRole(state, precompileAddr, enabledAddr, EnabledRole)
	}
	for _, adminAddr := range c.AdminAddresses {
		SetAllowListRole(state, precompileAddr, adminAddr, AdminRole)
	}
	return nil
}

// Equal returns true iff [other] has the same admins in the same order in its allow list.
func (c *AllowListConfig) Equal(other *AllowListConfig) bool {
	if other == nil {
		return false
	}

	return areEqualAddressLists(c.AdminAddresses, other.AdminAddresses) &&
		areEqualAddressLists(c.EnabledAddresses, other.EnabledAddresses)
}

// areEqualAddressLists returns true iff [a] and [b] have the same addresses in the same order.
func areEqualAddressLists(current []common.Address, other []common.Address) bool {
	if len(current) != len(other) {
		return false
	}
	for i, address := range current {
		if address != other[i] {
			return false
		}
	}
	return true
}

// Verify returns an error if there is an overlapping address between admin and enabled roles
func (c *AllowListConfig) Verify() error {
	addressMap := make(map[common.Address]Role) // tracks which addresses we have seen and their role

	// check for duplicates in enabled list
	for _, enabledAddr := range c.EnabledAddresses {
		if _, ok := addressMap[enabledAddr]; ok {
			return fmt.Errorf("duplicate address in enabled list: %s", enabledAddr)
		}
		addressMap[enabledAddr] = EnabledRole
	}

	// check for overlap between enabled and admin lists or duplicates in admin list
	for _, adminAddr := range c.AdminAddresses {
		if role, ok := addressMap[adminAddr]; ok {
			if role == AdminRole {
				return fmt.Errorf("duplicate address in admin list: %s", adminAddr)
			} else {
				return fmt.Errorf("cannot set address as both admin and enabled: %s", adminAddr)
			}
		}
		addressMap[adminAddr] = AdminRole
	}

	return nil
}
