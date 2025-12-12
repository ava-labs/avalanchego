// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package modules

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"
)

var (
	// registeredModules is a list of Module to preserve order
	// for deterministic iteration
	registeredModules = make([]Module, 0)

	reservedRanges = []utils.AddressRange{
		{
			Start: common.HexToAddress("0x0100000000000000000000000000000000000000"),
			End:   common.HexToAddress("0x01000000000000000000000000000000000000ff"),
		},
		{
			Start: common.HexToAddress("0x0200000000000000000000000000000000000000"),
			End:   common.HexToAddress("0x02000000000000000000000000000000000000ff"),
		},
		{
			Start: common.HexToAddress("0x0300000000000000000000000000000000000000"),
			End:   common.HexToAddress("0x03000000000000000000000000000000000000ff"),
		},
	}

	errBlackholeAddress          = fmt.Errorf("cannot register module that overlaps with blackhole address %s", constants.BlackholeAddr)
	errAddressNotInReservedRange = errors.New("address is not in a reserved range for custom precompiles")
)

// ReservedAddress returns true if [addr] is in a reserved range for custom precompiles
func ReservedAddress(addr common.Address) bool {
	for _, reservedRange := range reservedRanges {
		if reservedRange.Contains(addr) {
			return true
		}
	}

	return false
}

// RegisterModule registers a stateful precompile module
func RegisterModule(stm Module) error {
	address := stm.Address
	key := stm.ConfigKey

	if address == constants.BlackholeAddr {
		return fmt.Errorf("%w: address %s ", errBlackholeAddress, address)
	}
	if !ReservedAddress(address) {
		return fmt.Errorf("%w: address %s ", errAddressNotInReservedRange, address)
	}

	for _, registeredModule := range registeredModules {
		if registeredModule.ConfigKey == key {
			return fmt.Errorf("name %s already used by a stateful precompile", key)
		}
		if registeredModule.Address == address {
			return fmt.Errorf("address %s already used by a stateful precompile", address)
		}
	}
	// sort by address to ensure deterministic iteration
	registeredModules = insertSortedByAddress(registeredModules, stm)
	return nil
}

func GetPrecompileModuleByAddress(address common.Address) (Module, bool) {
	for _, stm := range registeredModules {
		if stm.Address == address {
			return stm, true
		}
	}
	return Module{}, false
}

func GetPrecompileModule(key string) (Module, bool) {
	for _, stm := range registeredModules {
		if stm.ConfigKey == key {
			return stm, true
		}
	}
	return Module{}, false
}

func RegisteredModules() []Module {
	return registeredModules
}

func insertSortedByAddress(data []Module, stm Module) []Module {
	data = append(data, stm)
	sort.Sort(moduleArray(data))
	return data
}
