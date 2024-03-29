// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/snow"
)

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

	// Unmarshal inlined PrecompileUpgrade
	return json.Unmarshal(data, &c.GenesisPrecompiles)
}

// MarshalJSON returns the JSON encoding of c.
// This is a custom marshaler to handle the Precompiles field.
func (c ChainConfig) MarshalJSON() ([]byte, error) {
	// Alias ChainConfig to avoid recursion
	type _ChainConfig ChainConfig
	tmp, err := json.Marshal(_ChainConfig(c))
	if err != nil {
		return nil, err
	}

	// To include PrecompileUpgrades, we unmarshal the json representing c
	// then directly add the corresponding keys to the json.
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(tmp, &raw); err != nil {
		return nil, err
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

// ToWithUpgradesJSON converts the ChainConfig to ChainConfigWithUpgradesJSON with upgrades explicitly displayed.
// ChainConfig does not include upgrades in its JSON output.
// This is a workaround for showing upgrades in the JSON output.
func (c *ChainConfig) ToWithUpgradesJSON() *ChainConfigWithUpgradesJSON {
	return &ChainConfigWithUpgradesJSON{
		ChainConfig:   *c,
		UpgradeConfig: c.UpgradeConfig,
	}
}

func (c *ChainConfig) SetNetworkUpgradeDefaults() {
	if c.HomesteadBlock == nil {
		c.HomesteadBlock = big.NewInt(0)
	}
	if c.EIP150Block == nil {
		c.EIP150Block = big.NewInt(0)
	}
	if c.EIP155Block == nil {
		c.EIP155Block = big.NewInt(0)
	}
	if c.EIP158Block == nil {
		c.EIP158Block = big.NewInt(0)
	}
	if c.ByzantiumBlock == nil {
		c.ByzantiumBlock = big.NewInt(0)
	}
	if c.ConstantinopleBlock == nil {
		c.ConstantinopleBlock = big.NewInt(0)
	}
	if c.PetersburgBlock == nil {
		c.PetersburgBlock = big.NewInt(0)
	}
	if c.IstanbulBlock == nil {
		c.IstanbulBlock = big.NewInt(0)
	}
	if c.MuirGlacierBlock == nil {
		c.MuirGlacierBlock = big.NewInt(0)
	}

	c.NetworkUpgrades.setDefaults(c.SnowCtx.NetworkID)
}

// SetMappedUpgrades sets the mapped upgrades (usually Avalanche > EVM upgrades) for the chain config.
func (c *ChainConfig) SetEVMUpgrades(avalancheUpgrades NetworkUpgrades) {
	// c.CancunTime = utils.NewUint64(*avalancheUpgrades.EUpgradeTimestamp)
}

func getUpgradeTime(networkID uint32, upgradeTimes map[uint32]time.Time) uint64 {
	if upgradeTime, ok := upgradeTimes[networkID]; ok {
		return uint64(upgradeTime.Unix())
	}
	// If the upgrade time isn't specified, default being enabled in the
	// genesis.
	return 0
}
