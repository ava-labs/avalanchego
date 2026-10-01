// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"

	evmparams "github.com/ava-labs/avalanchego/graft/evm/params"
	ethparams "github.com/ava-labs/libevm/params"
)

var (
	DefaultFeeConfig = commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 2, // in seconds

		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		BlockGasCostStep: big.NewInt(200_000),
	}

	SubnetEVMDefaultChainConfig = &ChainConfig{
		FeeConfig:          DefaultFeeConfig,
		NetworkUpgrades:    GetNetworkUpgrades(upgrade.GetConfig(constants.MainnetID)),
		GenesisPrecompiles: Precompiles{},
	}

	TestPreSubnetEVMChainConfig = &ChainConfig{
		FeeConfig:          DefaultFeeConfig,
		NetworkUpgrades:    NetworkUpgrades{},
		GenesisPrecompiles: Precompiles{},
	}

	TestSubnetEVMChainConfig = copyAndSet(TestPreSubnetEVMChainConfig, func(c *ChainConfig) {
		c.NetworkUpgrades.ApricotPhase1BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhase2BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhase3BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhase4BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhase5BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhasePre6BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhase6BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.ApricotPhasePost6BlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.BanffBlockTimestamp = utils.PointerTo[uint64](0)
		c.NetworkUpgrades.CortinaBlockTimestamp = utils.PointerTo[uint64](0)
	})

	TestDurangoChainConfig = copyAndSet(TestSubnetEVMChainConfig, func(c *ChainConfig) {
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

	TestChainConfig = copyConfig(TestIglooChainConfig)
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
	// Config for timestamps that enable network upgrades.
	NetworkUpgradeOverrides *NetworkUpgrades `json:"networkUpgradeOverrides,omitempty"`

	// Config for modifying state as a network upgrade.
	StateUpgrades []StateUpgrade `json:"stateUpgrades,omitempty"`

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

	FeeConfig          commontype.FeeConfig `json:"feeConfig"`                    // Set the configuration for the dynamic fee algorithm
	AllowFeeRecipients bool                 `json:"allowFeeRecipients,omitempty"` // Allows fees to be collected by block builders.
	GenesisPrecompiles Precompiles          `json:"-"`                            // Config for enabling precompiles from genesis. JSON encode/decode will be handled by the custom marshaler/unmarshaler.
	UpgradeConfig      `json:"-"`           // Config specified in upgradeBytes (avalanche network upgrades or enable/disabling precompiles). Not serialized.
	InitialMinDelayMS  uint64               `json:"initialMinDelayMS,omitempty"` // ACP-226 minimum block delay of the genesis block, in milliseconds. Requires Granite at genesis. Zero uses the ACP-226 default.
}

func (c *ChainConfig) CheckConfigCompatible(newConfig *ethparams.ChainConfig, headNumber *big.Int, headTimestamp uint64) *ethparams.ConfigCompatError {
	if c == nil {
		return nil
	}
	newcfg, ok := newConfig.Hooks().(*ChainConfig)
	if !ok {
		// Proper registration of the extras on the libevm side should prevent this from happening.
		// Return an error to prevent the chain from starting, just in case.
		return ethparams.NewTimestampCompatError(
			fmt.Sprintf("ChainConfig.Hooks() is not of the expected type *extras.ChainConfig, got %T", newConfig.Hooks()),
			utils.PointerTo[uint64](0),
			nil,
		)
	}
	return c.checkConfigCompatible(newcfg, headNumber, headTimestamp)
}

func (c *ChainConfig) checkConfigCompatible(newcfg *ChainConfig, _ *big.Int, headTimestamp uint64) *ethparams.ConfigCompatError {
	if err := c.NetworkUpgrades.CheckCompatible(&newcfg.NetworkUpgrades, headTimestamp); err != nil {
		return err
	}
	// Check that the precompiles on the new config are compatible with the existing precompile config.
	if err := c.checkPrecompilesCompatible(newcfg.PrecompileUpgrades, headTimestamp); err != nil {
		return err
	}

	// Check that the state upgrades on the new config are compatible with the existing state upgrade config.
	if err := c.checkStateUpgradesCompatible(newcfg.StateUpgrades, headTimestamp); err != nil {
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

	feeBytes, err := json.Marshal(c.FeeConfig)
	if err != nil {
		feeBytes = []byte("cannot marshal FeeConfig")
	}
	banner += fmt.Sprintf("Fee Config: %s\n", string(feeBytes))

	banner += fmt.Sprintf("Allow Fee Recipients: %v\n", c.AllowFeeRecipients)

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
// This is a custom unmarshaler to handle the Precompiles and NetworkUpgrades
// fields, which are presented inline in the JSON. The NetworkUpgrades are
// decoded by the codec registered with [evmparams.RegisterJSON].
// This custom unmarshaler ensures backwards compatibility with the old format.
func (c *ChainConfig) UnmarshalJSON(data []byte) error {
	var tmp ChainConfig
	if err := json.Unmarshal(data, &chainConfigJSON{_ChainConfig: (*_ChainConfig)(&tmp)}); err != nil {
		return err
	}
	if err := tmp.NetworkUpgrades.UnmarshalJSON(data); err != nil {
		return err
	}
	*c = tmp

	// Unmarshal inlined PrecompileUpgrade
	return json.Unmarshal(data, &c.GenesisPrecompiles)
}

// MarshalJSON returns the JSON encoding of c.
// This is a custom marshaler to inline the Precompiles and NetworkUpgrades
// fields.
//
// A value receiver is used so the method isn't shadowed by the one promoted
// from [NetworkUpgrades] when marshalling a non-pointer ChainConfig.
func (c ChainConfig) MarshalJSON() ([]byte, error) {
	raw, err := toRawMap(chainConfigJSON{_ChainConfig: (*_ChainConfig)(&c)})
	if err != nil {
		return nil, err
	}
	upgrades, err := toRawMap(c.NetworkUpgrades)
	if err != nil {
		return nil, err
	}
	for key, value := range upgrades {
		raw[key] = value
	}

	for key, value := range c.GenesisPrecompiles {
		conf, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		raw[key] = conf
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
	return c.NetworkUpgrades.CheckForkOrder(evmparams.Fortuna)
}

var errInitialMinDelayTooLarge = errors.New("initialMinDelayMS too large")

// Verify verifies chain config.
func (c *ChainConfig) Verify() error {
	if err := c.FeeConfig.Verify(); err != nil {
		return fmt.Errorf("invalid fee config: %w", err)
	}

	// Verify the precompile upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyPrecompileUpgrades(); err != nil {
		return fmt.Errorf("invalid precompile upgrades: %w", err)
	}
	// Verify the state upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyStateUpgrades(); err != nil {
		return fmt.Errorf("invalid state upgrades: %w", err)
	}

	// Verify the network upgrades are internally consistent given the existing chainConfig.
	if err := verifyNetworkUpgrades(&c.NetworkUpgrades, c.SnowCtx.NetworkUpgrades); err != nil {
		return fmt.Errorf("invalid network upgrades: %w", err)
	}

	if maxDelayMS := acp226.InitialDelayExcess.Delay(); c.InitialMinDelayMS > maxDelayMS {
		return fmt.Errorf("%w: %d exceeds %d", errInitialMinDelayTooLarge, c.InitialMinDelayMS, maxDelayMS)
	}

	return nil
}

// IsPrecompileEnabled returns whether precompile with `address` is enabled at `timestamp`.
func (c *ChainConfig) IsPrecompileEnabled(address common.Address, timestamp uint64) bool {
	config := c.GetActivePrecompileConfig(address, timestamp)
	return config != nil && !config.IsDisabled()
}

// GetFeeConfig returns the original FeeConfig contained in the genesis ChainConfig.
// Implements precompile.ChainConfig interface.
func (c *ChainConfig) GetFeeConfig() commontype.FeeConfig {
	return c.FeeConfig
}

// AllowedFeeRecipients returns the original AllowedFeeRecipients parameter contained in the genesis ChainConfig.
// Implements precompile.ChainConfig interface.
func (c *ChainConfig) AllowedFeeRecipients() bool {
	return c.AllowFeeRecipients
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
