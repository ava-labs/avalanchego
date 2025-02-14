// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

const (
	maxJSONLen = 64 * 1024 * 1024 // 64MB

)

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

// SetEthUpgrades enables Etheruem network upgrades using the same time as
// the Avalanche network upgrade that enables them.
func (c *ChainConfig) SetEthUpgrades() {
	// Set Ethereum block upgrades to initially activated as they were already activated on launch.
	c.HomesteadBlock = big.NewInt(0)
	c.DAOForkBlock = big.NewInt(0)
	c.DAOForkSupport = true
	c.EIP150Block = big.NewInt(0)
	c.EIP155Block = big.NewInt(0)
	c.EIP158Block = big.NewInt(0)
	c.ByzantiumBlock = big.NewInt(0)
	c.ConstantinopleBlock = big.NewInt(0)
	c.PetersburgBlock = big.NewInt(0)
	c.IstanbulBlock = big.NewInt(0)
	c.MuirGlacierBlock = big.NewInt(0)

	if c.ChainID != nil && AvalancheFujiChainID.Cmp(c.ChainID) == 0 {
		c.BerlinBlock = big.NewInt(184985) // https://testnet.snowtrace.io/block/184985?chainid=43113, AP2 activation block
		c.LondonBlock = big.NewInt(805078) // https://testnet.snowtrace.io/block/805078?chainid=43113, AP3 activation block
	} else if c.ChainID != nil && AvalancheMainnetChainID.Cmp(c.ChainID) == 0 {
		c.BerlinBlock = big.NewInt(1640340) // https://snowtrace.io/block/1640340?chainid=43114, AP2 activation block
		c.LondonBlock = big.NewInt(3308552) // https://snowtrace.io/block/3308552?chainid=43114, AP3 activation block
	} else {
		// In testing or local networks, we only support enabling Berlin and London prior
		// to the initially active time. This is likely to correspond to an intended block
		// number of 0 as well.
		initiallyActive := uint64(upgrade.InitiallyActiveTime.Unix())
		if c.ApricotPhase2BlockTimestamp != nil && *c.ApricotPhase2BlockTimestamp <= initiallyActive && c.BerlinBlock == nil {
			c.BerlinBlock = big.NewInt(0)
		}
		if c.ApricotPhase3BlockTimestamp != nil && *c.ApricotPhase3BlockTimestamp <= initiallyActive && c.LondonBlock == nil {
			c.LondonBlock = big.NewInt(0)
		}
	}
	if c.DurangoBlockTimestamp != nil {
		c.ShanghaiTime = utils.NewUint64(*c.DurangoBlockTimestamp)
	}
	if c.EtnaTimestamp != nil {
		c.CancunTime = utils.NewUint64(*c.EtnaTimestamp)
	}
}

// UnmarshalJSON parses the JSON-encoded data and stores the result in the
// object pointed to by c.
// This is a custom unmarshaler to handle the Precompiles field.
// Precompiles was presented as an inline object in the JSON.
// This custom unmarshaler ensures backwards compatibility with the old format.
func (c *ChainConfig) UnmarshalJSON(data []byte) error {
	// Alias ChainConfig to avoid recursion
	type _ChainConfig ChainConfig
	tmp := _ChainConfig{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	// At this point we have populated all fields except PrecompileUpgrade
	*c = ChainConfig(tmp)

	return nil
}

// MarshalJSON returns the JSON encoding of c.
// This is a custom marshaler to handle the Precompiles field.
func (c ChainConfig) MarshalJSON() ([]byte, error) {
	// Alias ChainConfig to avoid recursion
	type _ChainConfig ChainConfig
	return json.Marshal(_ChainConfig(c))
}

type ChainConfigWithUpgradesJSON struct {
	ChainConfig
	UpgradeConfig UpgradeConfig `json:"upgrades,omitempty"`
}

// MarshalJSON implements json.Marshaler. This is a workaround for the fact that
// the embedded ChainConfig struct has a MarshalJSON method, which prevents
// the default JSON marshalling from working for UpgradeConfig.
// TODO: consider removing this method by allowing external tag for the embedded
// ChainConfig struct.
func (cu ChainConfigWithUpgradesJSON) MarshalJSON() ([]byte, error) {
	// embed the ChainConfig struct into the response
	chainConfigJSON, err := json.Marshal(cu.ChainConfig)
	if err != nil {
		return nil, err
	}
	if len(chainConfigJSON) > maxJSONLen {
		return nil, errors.New("value too large")
	}

	type upgrades struct {
		UpgradeConfig UpgradeConfig `json:"upgrades"`
	}

	upgradeJSON, err := json.Marshal(upgrades{cu.UpgradeConfig})
	if err != nil {
		return nil, err
	}
	if len(upgradeJSON) > maxJSONLen {
		return nil, errors.New("value too large")
	}

	// merge the two JSON objects
	mergedJSON := make([]byte, 0, len(chainConfigJSON)+len(upgradeJSON)+1)
	mergedJSON = append(mergedJSON, chainConfigJSON[:len(chainConfigJSON)-1]...)
	mergedJSON = append(mergedJSON, ',')
	mergedJSON = append(mergedJSON, upgradeJSON[1:]...)
	return mergedJSON, nil
}

func (cu *ChainConfigWithUpgradesJSON) UnmarshalJSON(input []byte) error {
	var cc ChainConfig
	if err := json.Unmarshal(input, &cc); err != nil {
		return err
	}

	type upgrades struct {
		UpgradeConfig UpgradeConfig `json:"upgrades"`
	}

	var u upgrades
	if err := json.Unmarshal(input, &u); err != nil {
		return err
	}
	cu.ChainConfig = cc
	cu.UpgradeConfig = u.UpgradeConfig
	return nil
}

// Verify verifies chain config and returns error
func (c *ChainConfig) Verify() error {
	// Verify the precompile upgrades are internally consistent given the existing chainConfig.
	if err := c.verifyPrecompileUpgrades(); err != nil {
		return fmt.Errorf("invalid precompile upgrades: %w", err)
	}

	return nil
}

// IsPrecompileEnabled returns whether precompile with [address] is enabled at [timestamp].
func (c *ChainConfig) IsPrecompileEnabled(address common.Address, timestamp uint64) bool {
	config := c.getActivePrecompileConfig(address, timestamp)
	return config != nil && !config.IsDisabled()
}

// ToWithUpgradesJSON converts the ChainConfig to ChainConfigWithUpgradesJSON with upgrades explicitly displayed.
// ChainConfig does not include upgrades in its JSON output.
// This is a workaround for showing upgrades in the JSON output.
func (c *ChainConfig) ToWithUpgradesJSON() *ChainConfigWithUpgradesJSON {
	return &ChainConfigWithUpgradesJSON{
		ChainConfig:   *c,
		UpgradeConfig: c.UpgradeConfig,
	}
}

func (r *Rules) PredicatersExist() bool {
	return len(r.Predicaters) > 0
}

func (r *Rules) PredicaterExists(addr common.Address) bool {
	_, PredicaterExists := r.Predicaters[addr]
	return PredicaterExists
}

// IsPrecompileEnabled returns true if the precompile at [addr] is enabled for this rule set.
func (r *Rules) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.ActivePrecompiles[addr]
	return ok
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return fmt.Sprintf("%d", *val)
}

// IsForkTransition returns true if [fork] activates during the transition from
// [parent] to [current].
// Taking [parent] as a pointer allows for us to pass nil when checking forks
// that activate during genesis.
// Note: this works for both block number and timestamp activated forks.
func IsForkTransition(fork *uint64, parent *uint64, current uint64) bool {
	var parentForked bool
	if parent != nil {
		parentForked = isTimestampForked(fork, *parent)
	}
	currentForked := isTimestampForked(fork, current)
	return !parentForked && currentForked
}
