// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/utils"
)

const (
	maxJSONLen = 64 * 1024 * 1024 // 64MB

	// TODO: Value to pass to geth's Rules by default where the appropriate
	// context is not available in the avalanche code. (similar to context.TODO())
	IsMergeTODO = true
)

// SetEthUpgrades enables Etheruem network upgrades using the same time as
// the Avalanche network upgrade that enables them.
func SetEthUpgrades(c *ChainConfig) {
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
		extra := GetExtra(c)
		if extra.ApricotPhase2BlockTimestamp != nil && *extra.ApricotPhase2BlockTimestamp <= initiallyActive && c.BerlinBlock == nil {
			c.BerlinBlock = big.NewInt(0)
		}
		if extra.ApricotPhase3BlockTimestamp != nil && *extra.ApricotPhase3BlockTimestamp <= initiallyActive && c.LondonBlock == nil {
			c.LondonBlock = big.NewInt(0)
		}
	}
	extra := GetExtra(c)
	if extra.DurangoBlockTimestamp != nil {
		c.ShanghaiTime = utils.NewUint64(*extra.DurangoBlockTimestamp)
	}
	if extra.EtnaTimestamp != nil {
		c.CancunTime = utils.NewUint64(*extra.EtnaTimestamp)
	}
}

func GetExtra(c *ChainConfig) *extras.ChainConfig {
	ex := payloads.ChainConfig.Get(c)
	if ex == nil {
		ex = &extras.ChainConfig{}
		payloads.ChainConfig.Set(c, ex)
	}
	return ex
}

func Copy(c *ChainConfig) ChainConfig {
	cpy := *c
	extraCpy := *GetExtra(c)
	return *WithExtra(&cpy, &extraCpy)
}

// WithExtra sets the extra payload on `c` and returns the modified argument.
func WithExtra(c *ChainConfig, extra *extras.ChainConfig) *ChainConfig {
	payloads.ChainConfig.Set(c, extra)
	return c
}

type ChainConfigWithUpgradesJSON struct {
	ChainConfig
	UpgradeConfig extras.UpgradeConfig `json:"upgrades,omitempty"`
}

// MarshalJSON implements json.Marshaler. This is a workaround for the fact that
// the embedded ChainConfig struct has a MarshalJSON method, which prevents
// the default JSON marshalling from working for UpgradeConfig.
// TODO: consider removing this method by allowing external tag for the embedded
// ChainConfig struct.
func (cu ChainConfigWithUpgradesJSON) MarshalJSON() ([]byte, error) {
	// embed the ChainConfig struct into the response
	chainConfigJSON, err := json.Marshal(&cu.ChainConfig)
	if err != nil {
		return nil, err
	}
	if len(chainConfigJSON) > maxJSONLen {
		return nil, errors.New("value too large")
	}

	type upgrades struct {
		UpgradeConfig extras.UpgradeConfig `json:"upgrades"`
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
		UpgradeConfig extras.UpgradeConfig `json:"upgrades"`
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
func ToWithUpgradesJSON(c *ChainConfig) *ChainConfigWithUpgradesJSON {
	return &ChainConfigWithUpgradesJSON{
		ChainConfig:   *c,
		UpgradeConfig: GetExtra(c).UpgradeConfig,
	}
}
