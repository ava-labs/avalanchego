// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
)

var ErrCannotAddManagersBeforeDurango = errors.New("cannot add managers before Durango")

// AllowListConfig specifies the initial set of addresses with Admin or Enabled roles.
type AllowListConfig struct {
	AdminAddresses   []common.Address `json:"adminAddresses,omitempty"`   // initial admin addresses
	ManagerAddresses []common.Address `json:"managerAddresses,omitempty"` // initial manager addresses
	EnabledAddresses []common.Address `json:"enabledAddresses,omitempty"` // initial enabled addresses
}

// Configure initializes the address space of [precompileAddr] by initializing the role of each of
// the addresses in [AllowListAdmins].
func (c *AllowListConfig) Configure(_ precompileconfig.ChainConfig, precompileAddr common.Address, state contract.StateDB, _ contract.ConfigurationBlockContext) error {
	for _, enabledAddr := range c.EnabledAddresses {
		SetAllowListRole(state, precompileAddr, enabledAddr, EnabledRole)
	}
	for _, adminAddr := range c.AdminAddresses {
		SetAllowListRole(state, precompileAddr, adminAddr, AdminRole)
	}
	// Verify() should have been called before Configure()
	// so we know manager role is activated
	for _, managerAddr := range c.ManagerAddresses {
		SetAllowListRole(state, precompileAddr, managerAddr, ManagerRole)
	}
	return nil
}

// Equal returns true iff [other] has the same admins in the same order in its allow list.
func (c *AllowListConfig) Equal(other *AllowListConfig) bool {
	if other == nil {
		return false
	}

	return areEqualAddressLists(c.AdminAddresses, other.AdminAddresses) &&
		areEqualAddressLists(c.ManagerAddresses, other.ManagerAddresses) &&
		areEqualAddressLists(c.EnabledAddresses, other.EnabledAddresses)
}

// areEqualAddressLists returns true iff [a] and [b] have the same addresses in the same order.
func areEqualAddressLists(current []common.Address, other []common.Address) bool {
	return slices.Equal(current, other)
}

// Verify returns an error if there is an overlapping address between admin and enabled roles
func (c *AllowListConfig) Verify(chainConfig precompileconfig.ChainConfig, upgrade precompileconfig.Upgrade) error {
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

	if len(c.ManagerAddresses) != 0 && upgrade.Timestamp() != nil {
		// If the config attempts to activate a manager before the Durango, fail verification
		timestamp := *upgrade.Timestamp()
		if !chainConfig.IsDurango(timestamp) {
			return ErrCannotAddManagersBeforeDurango
		}
	}

	// check for overlap between admin and manager lists or duplicates in manager list
	for _, managerAddr := range c.ManagerAddresses {
		if role, ok := addressMap[managerAddr]; ok {
			switch role {
			case ManagerRole:
				return fmt.Errorf("duplicate address in manager list: %s", managerAddr)
			case AdminRole:
				return fmt.Errorf("cannot set address as both admin and manager: %s", managerAddr)
			case EnabledRole:
				return fmt.Errorf("cannot set address as both enabled and manager: %s", managerAddr)
			}
		}
		addressMap[managerAddr] = ManagerRole
	}

	return nil
}
