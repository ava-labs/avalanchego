// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var (
	_ precompileconfig.Config = (*Config)(nil)

	ErrInitialMintNilAmount     = errors.New("initial mint cannot contain nil amount")
	ErrInitialMintInvalidAmount = errors.New("initial mint cannot contain invalid amount")
)

// Config implements the precompileconfig.Config interface while adding in the
// ContractNativeMinter specific precompile config.
type Config struct {
	allowlist.AllowListConfig
	precompileconfig.Upgrade

	InitialMint map[common.Address]*math.HexOrDecimal256 `json:"initialMint,omitempty"` // addresses to receive the initial mint mapped to the amount to mint
}

// NewConfig returns a config for a network upgrade at [blockTimestamp] that enables
// ContractNativeMinter with the given [admins], [enableds] and [managers] as members of the allowlist.
// Also mints balances according to [initialMint] when the upgrade activates.
func NewConfig(blockTimestamp *uint64, admins []common.Address, enableds []common.Address, managers []common.Address, initialMint map[common.Address]*math.HexOrDecimal256) *Config {
	return &Config{
		AllowListConfig: allowlist.AllowListConfig{
			AdminAddresses:   admins,
			EnabledAddresses: enableds,
			ManagerAddresses: managers,
		},
		Upgrade:     precompileconfig.Upgrade{BlockTimestamp: blockTimestamp},
		InitialMint: initialMint,
	}
}

// NewDisableConfig returns config for a network upgrade at [blockTimestamp]
// that disables ContractNativeMinter.
func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Key returns the key for the ContractNativeMinter precompileconfig.
// This should be the same key as used in the precompile module.
func (*Config) Key() string { return ConfigKey }

// Equal returns true if [cfg] is a [*ContractNativeMinterConfig] and it has been configured identical to [c].
func (c *Config) Equal(cfg precompileconfig.Config) bool {
	// typecast before comparison
	other, ok := (cfg).(*Config)
	if !ok {
		return false
	}
	eq := c.Upgrade.Equal(&other.Upgrade) && c.AllowListConfig.Equal(&other.AllowListConfig)
	if !eq {
		return false
	}

	if len(c.InitialMint) != len(other.InitialMint) {
		return false
	}

	for address, amount := range c.InitialMint {
		val, ok := other.InitialMint[address]
		if !ok {
			return false
		}
		bigIntAmount := (*big.Int)(amount)
		bigIntVal := (*big.Int)(val)
		if !utils.BigEqual(bigIntAmount, bigIntVal) {
			return false
		}
	}

	return true
}

func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	// ensure that all of the initial mint values in the map are non-nil positive values
	for addr, amount := range c.InitialMint {
		if amount == nil {
			return fmt.Errorf("%w for address %s", ErrInitialMintNilAmount, addr)
		}
		bigIntAmount := (*big.Int)(amount)
		if bigIntAmount.Sign() < 1 {
			return fmt.Errorf("%w: amount %v for address %s", ErrInitialMintInvalidAmount, bigIntAmount, addr)
		}
	}
	return c.AllowListConfig.Verify(chainConfig, c.Upgrade)
}
