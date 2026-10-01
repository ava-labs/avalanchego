// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	TestIglooChainConfig = copyAndSet(TestHeliconChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.IglooTimestamp = utils.PointerTo[uint64](0)
	})

	TestChainConfig = copyConfig(TestGraniteChainConfig)
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
	NetworkUpgrades `json:"-"` // Config for timestamps that enable network upgrades. JSON encode/decode will be handled by the custom marshaler/unmarshaler.

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

	if err := c.NetworkUpgrades.CheckCompatible(&newcfg.NetworkUpgrades, headTimestamp); err != nil {
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

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

// chainConfigJSON is a [ChainConfig] without its JSON methods, nor those
// promoted from the embedded [NetworkUpgrades], so it uses the default
// encoding for all fields other than the upgrades.
type chainConfigJSON struct {
	*_ChainConfig

	// Shadow the methods promoted from [NetworkUpgrades].
	MarshalJSON   struct{} `json:"-"`
	UnmarshalJSON struct{} `json:"-"`
}

type _ChainConfig ChainConfig

// UnmarshalJSON parses the JSON-encoded data and stores the result in the
// object pointed to by c.
// The [NetworkUpgrades] are presented inline in the JSON and decoded by the
// codec registered with [evmparams.RegisterJSON].
func (c *ChainConfig) UnmarshalJSON(data []byte) error {
	var tmp ChainConfig
	if err := json.Unmarshal(data, &chainConfigJSON{_ChainConfig: (*_ChainConfig)(&tmp)}); err != nil {
		return err
	}
	if err := tmp.NetworkUpgrades.UnmarshalJSON(data); err != nil {
		return err
	}
	*c = tmp
	return nil
}

// MarshalJSON returns the JSON encoding of c.
// The [NetworkUpgrades] are inlined using the codec registered with
// [evmparams.RegisterJSON].
//
// A value receiver is used so the method isn't shadowed by the one promoted
// from [NetworkUpgrades] when marshalling a non-pointer ChainConfig.
func (c ChainConfig) MarshalJSON() ([]byte, error) {
	return marshalWithUpgrades(chainConfigJSON{_ChainConfig: (*_ChainConfig)(&c)}, c.NetworkUpgrades)
}

// marshalWithUpgrades returns the JSON encoding of v, with the keys of the
// encoded upgrades added to the root object.
func marshalWithUpgrades(v any, upgrades NetworkUpgrades) ([]byte, error) {
	raw, err := toRawMap(v)
	if err != nil {
		return nil, err
	}
	upgradesRaw, err := toRawMap(upgrades)
	if err != nil {
		return nil, err
	}
	for k, v := range upgradesRaw {
		raw[k] = v
	}
	return json.Marshal(raw)
}

func toRawMap(v any) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	return raw, nil
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
	return c.NetworkUpgrades.CheckForkOrder()
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
