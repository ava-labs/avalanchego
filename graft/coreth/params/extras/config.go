// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/snow"

	ethparams "github.com/ava-labs/libevm/params"
)

var (
	TestLaunchConfig = &ChainConfig{}

	TestApricotPhase1Config = copyAndSet(TestLaunchConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase1BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhase2Config = copyAndSet(TestApricotPhase1Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase2BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhase3Config = copyAndSet(TestApricotPhase2Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase3BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhase4Config = copyAndSet(TestApricotPhase3Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase4BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhase5Config = copyAndSet(TestApricotPhase4Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase5BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhasePre6Config = copyAndSet(TestApricotPhase5Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhasePre6BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhase6Config = copyAndSet(TestApricotPhasePre6Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase6BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestApricotPhasePost6Config = copyAndSet(TestApricotPhase6Config, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhasePost6BlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestBanffChainConfig = copyAndSet(TestApricotPhasePost6Config, func(c *ChainConfig) {
		c.NetworkUpgrades.BanffBlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestCortinaChainConfig = copyAndSet(TestBanffChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.CortinaBlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestDurangoChainConfig = copyAndSet(TestCortinaChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.DurangoBlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestEtnaChainConfig = copyAndSet(TestDurangoChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.EtnaTimestamp = utils.PointerTo[uint64](0)
	})

	TestFortunaChainConfig = copyAndSet(TestEtnaChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.FortunaTimestamp = utils.PointerTo[uint64](0)
	})

	TestGraniteChainConfig = copyAndSet(TestFortunaChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.GraniteTimestamp = utils.PointerTo[uint64](0)
	})

	TestHeliconChainConfig = copyAndSet(TestGraniteChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.HeliconTimestamp = utils.PointerTo[uint64](0)
	})

	TestChainConfig = copyConfig(TestHeliconChainConfig)
)

func copyConfig(c *ChainConfig) *ChainConfig {
	newConfig := *c
	return &newConfig
}

func copyAndSet(c *ChainConfig, set func(*ChainConfig)) *ChainConfig {
	newConfig := *c
	set(&newConfig)
	return &newConfig
}

// UpgradeConfig includes the following configs that may be specified in upgradeBytes:
// - Timestamps that enable avalanche network upgrades,
// - Enabling or disabling precompiles as network upgrades.
type UpgradeConfig struct {
	// Config for enabling and disabling precompiles as network upgrades.
	PrecompileUpgrades []PrecompileUpgrade `json:"precompileUpgrades,omitempty"`
}

// AvalancheContext provides Avalanche specific context directly into the EVM.
type AvalancheContext struct {
	SnowCtx *snow.Context
}

type ChainConfig struct {
	NetworkUpgrades // Config for timestamps that enable network upgrades.

	AvalancheContext `json:"-"` // Avalanche specific context set during VM initialization. Not serialized.

	UpgradeConfig `json:"-"` // Config specified in upgradeBytes (avalanche network upgrades or enable/disabling precompiles). Not serialized.
}

//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (c *ChainConfig) CheckConfigCompatible(newcfg_ *ethparams.ChainConfig, headNumber *big.Int, headTimestamp uint64) *ethparams.ConfigCompatError {
	if c == nil {
		return nil
	}
	newcfg, ok := newcfg_.Hooks().(*ChainConfig)
	if !ok {
		// Proper registration of the extras on the libevm side should prevent this from happening.
		// Return an error to prevent the chain from starting, just in case.
		return ethparams.NewTimestampCompatError(
			fmt.Sprintf("ChainConfig.Hooks() is not of the expected type *extras.ChainConfig, got %T", newcfg_.Hooks()),
			utils.PointerTo[uint64](0),
			nil,
		)
	}

	if err := c.checkNetworkUpgradesCompatible(&newcfg.NetworkUpgrades, headTimestamp); err != nil {
		return err
	}

	return nil
}

func (c *ChainConfig) Description() string {
	if c == nil {
		return ""
	}
	var banner string

	banner += "Avalanche Upgrades (timestamp based):\n"
	banner += c.NetworkUpgrades.Description()
	banner += "\n"

	upgradeConfigBytes, err := json.Marshal(c.UpgradeConfig)
	if err != nil {
		upgradeConfigBytes = []byte("cannot marshal UpgradeConfig")
	}
	banner += "Upgrade Config: " + string(upgradeConfigBytes)
	banner += "\n"
	return banner
}

// isForkTimestampIncompatible returns true if a fork scheduled at timestamp s1
// cannot be rescheduled to timestamp s2 because head is already past the fork.
func isForkTimestampIncompatible(s1, s2 *uint64, head uint64) bool {
	return (isTimestampForked(s1, head) || isTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
}

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

// UnmarshalJSON parses the JSON-encoded data and stores the result in the
// object pointed to by c.
// This is a custom unmarshaler to handle the Precompiles field.
// Precompiles was presented as an inline object in the JSON.
// This custom unmarshaler ensures backwards compatibility with the old format.
func (c *ChainConfig) UnmarshalJSON(data []byte) error {
	// Alias ChainConfigExtra to avoid recursion
	type _ChainConfigExtra ChainConfig
	tmp := _ChainConfigExtra{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// At this point we have populated all fields except PrecompileUpgrade
	*c = ChainConfig(tmp)

	return nil
}

// MarshalJSON returns the JSON encoding of c.
// This is a custom marshaler to handle the Precompiles field.
func (c *ChainConfig) MarshalJSON() ([]byte, error) {
	// Alias ChainConfigExtra to avoid recursion
	type _ChainConfigExtra ChainConfig
	return json.Marshal(_ChainConfigExtra(*c))
}

type fork struct {
	name      string
	block     *big.Int // some go-ethereum forks use block numbers
	timestamp *uint64  // Avalanche forks use timestamps
	optional  bool     // if true, the fork may be nil and next fork is still allowed
}

func (c *ChainConfig) CheckConfigForkOrder() error {
	if c == nil {
		return nil
	}
	// Note: In Avalanche, upgrades must take place via block timestamps instead
	// of block numbers since blocks are produced asynchronously. Therefore, we do
	// not check block timestamp forks in the same way as block number forks since
	// it would not be a meaningful comparison. Instead, we only check that the
	// Avalanche upgrades are enabled in order.
	// Note: we do not add the precompile configs here because they are optional
	// and independent, i.e. the order in which they are enabled does not impact
	// the correctness of the chain config.
	return checkForks(c.forkOrder())
}

// checkForks checks that forks are enabled in order and returns an error if not.
// `blockFork` is true if the fork is a block number fork, false if it is a timestamp fork
func checkForks(forks []fork) error {
	lastFork := fork{}
	for _, cur := range forks {
		if lastFork.name != "" {
			switch {
			// Non-optional forks must all be present in the chain config up to the last defined fork
			case lastFork.block == nil && lastFork.timestamp == nil && (cur.block != nil || cur.timestamp != nil):
				if cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at block %v",
						lastFork.name, cur.name, cur.block)
				} else {
					return fmt.Errorf("unsupported fork ordering: %v not enabled, but %v enabled at timestamp %v",
						lastFork.name, cur.name, cur.timestamp)
				}

			// Fork (whether defined by block or timestamp) must follow the fork definition sequence
			case (lastFork.block != nil && cur.block != nil) || (lastFork.timestamp != nil && cur.timestamp != nil):
				if lastFork.block != nil && lastFork.block.Cmp(cur.block) > 0 {
					return fmt.Errorf("unsupported fork ordering: %v enabled at block %v, but %v enabled at block %v",
						lastFork.name, lastFork.block, cur.name, cur.block)
				} else if lastFork.timestamp != nil && *lastFork.timestamp > *cur.timestamp {
					return fmt.Errorf("unsupported fork ordering: %v enabled at timestamp %v, but %v enabled at timestamp %v",
						lastFork.name, lastFork.timestamp, cur.name, cur.timestamp)
				}

				// Timestamp based forks can follow block based ones, but not the other way around
				if lastFork.timestamp != nil && cur.block != nil {
					return fmt.Errorf("unsupported fork ordering: %v used timestamp ordering, but %v reverted to block ordering",
						lastFork.name, cur.name)
				}
			}
		}
		// If it was optional and not set, then ignore it
		if !cur.optional || (cur.block != nil || cur.timestamp != nil) {
			lastFork = cur
		}
	}
	return nil
}

// Verify verifies chain config.
func (c *ChainConfig) Verify() error {
	// Verify the precompile upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyPrecompileUpgrades(); err != nil {
		return fmt.Errorf("invalid precompile upgrades: %w", err)
	}

	return nil
}

// IsPrecompileEnabled returns whether precompile with `address` is enabled at `timestamp`.
func (c *ChainConfig) IsPrecompileEnabled(address common.Address, timestamp uint64) bool {
	config := c.GetActivePrecompileConfig(address, timestamp)
	return config != nil && !config.IsDisabled()
}

// IsForkTransition returns true if `fork` activates during the transition from
// `parent` to `current`.
// Taking `parent` as a pointer allows for us to pass nil when checking forks
// that activate during genesis.
// Note: `parent` and `current` can be either both timestamp values, or both
// block number values, since this function works for both block number and
// timestamp activated forks.
func IsForkTransition(fork *uint64, parent *uint64, current uint64) bool {
	var parentForked bool
	if parent != nil {
		parentForked = isTimestampForked(fork, *parent)
	}
	currentForked := isTimestampForked(fork, current)
	return !parentForked && currentForked
}
