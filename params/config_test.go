// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new                 *ChainConfig
		blockHeight, blockTimestamp uint64
		wantErr                     *ConfigCompatError
	}
	tests := []test{
		{stored: TestChainConfig, new: TestChainConfig, blockHeight: 0, blockTimestamp: 0, wantErr: nil},
		{stored: TestChainConfig, new: TestChainConfig, blockHeight: 100, blockTimestamp: 1000, wantErr: nil},
		{
			stored:         &ChainConfig{EIP150Block: big.NewInt(10)},
			new:            &ChainConfig{EIP150Block: big.NewInt(20)},
			blockHeight:    9,
			blockTimestamp: 90,
			wantErr:        nil,
		},
		{
			stored:         TestChainConfig,
			new:            &ChainConfig{HomesteadBlock: nil},
			blockHeight:    3,
			blockTimestamp: 30,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored:         TestChainConfig,
			new:            &ChainConfig{HomesteadBlock: big.NewInt(1)},
			blockHeight:    3,
			blockTimestamp: 30,
			wantErr: &ConfigCompatError{
				What:         "Homestead fork block",
				StoredConfig: big.NewInt(0),
				NewConfig:    big.NewInt(1),
				RewindTo:     0,
			},
		},
		{
			stored:         &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:            &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			blockHeight:    25,
			blockTimestamp: 250,
			wantErr: &ConfigCompatError{
				What:         "EIP150 fork block",
				StoredConfig: big.NewInt(10),
				NewConfig:    big.NewInt(20),
				RewindTo:     9,
			},
		},
		{
			stored:         &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:            &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			blockHeight:    40,
			blockTimestamp: 400,
			wantErr:        nil,
		},
		{
			stored:         &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:            &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			blockHeight:    40,
			blockTimestamp: 400,
			wantErr: &ConfigCompatError{
				What:         "Petersburg fork block",
				StoredConfig: nil,
				NewConfig:    big.NewInt(31),
				RewindTo:     30,
			},
		},
		{
			stored:         TestChainConfig,
			new:            TestPreSubnetEVMConfig,
			blockHeight:    0,
			blockTimestamp: 0,
			wantErr: &ConfigCompatError{
				What:         "SubnetEVM fork block timestamp",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
		{
			stored:         TestChainConfig,
			new:            TestPreSubnetEVMConfig,
			blockHeight:    10,
			blockTimestamp: 100,
			wantErr: &ConfigCompatError{
				What:         "SubnetEVM fork block timestamp",
				StoredConfig: big.NewInt(0),
				NewConfig:    nil,
				RewindTo:     0,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.blockHeight, test.blockTimestamp)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nblockHeight: %v\nerr: %v\nwant: %v", test.stored, test.new, test.blockHeight, err, test.wantErr)
		}
	}
}

func TestConfigUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	testRewardManagerConfig := rewardmanager.NewConfig(
		big.NewInt(1671542573),
		[]common.Address{common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")},
		nil,
		&rewardmanager.InitialRewardConfig{
			AllowFeeRecipients: true,
		})

	testContractNativeMinterConfig := nativeminter.NewConfig(
		big.NewInt(0),
		[]common.Address{common.HexToAddress("0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC")},
		nil,
		nil,
	)

	config := []byte(`
	{
		"chainId": 43214,
		"allowFeeRecipients": true,
		"rewardManagerConfig": {
			"blockTimestamp": 1671542573,
			"adminAddresses": [
				"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
			],
			"initialRewardConfig": {
				"allowFeeRecipients": true
			}
		},
		"contractNativeMinterConfig": {
			"blockTimestamp": 0,
			"adminAddresses": [
				"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
			]
		}
	}
	`)
	c := ChainConfig{}
	err := json.Unmarshal(config, &c)
	require.NoError(err)

	require.Equal(c.ChainID, big.NewInt(43214))
	require.Equal(c.AllowFeeRecipients, true)

	rewardManagerConfig, ok := c.GenesisPrecompiles[rewardmanager.ConfigKey]
	require.True(ok)
	require.Equal(rewardManagerConfig.Key(), rewardmanager.ConfigKey)
	require.True(rewardManagerConfig.Equal(testRewardManagerConfig))

	nativeMinterConfig := c.GenesisPrecompiles[nativeminter.ConfigKey]
	require.Equal(nativeMinterConfig.Key(), nativeminter.ConfigKey)
	require.True(nativeMinterConfig.Equal(testContractNativeMinterConfig))

	// Marshal and unmarshal again and check that the result is the same
	marshaled, err := json.Marshal(c)
	require.NoError(err)
	c2 := ChainConfig{}
	err = json.Unmarshal(marshaled, &c2)
	require.NoError(err)
	require.Equal(c, c2)
}

func TestActivePrecompiles(t *testing.T) {
	config := ChainConfig{
		UpgradeConfig: UpgradeConfig{
			PrecompileUpgrades: []PrecompileUpgrade{
				{
					nativeminter.NewConfig(common.Big0, nil, nil, nil), // enable at genesis
				},
				{
					nativeminter.NewDisableConfig(common.Big1), // disable at timestamp 1
				},
			},
		},
	}

	rules0 := config.AvalancheRules(common.Big0, common.Big0)
	require.True(t, rules0.IsPrecompileEnabled(nativeminter.Module.Address))

	rules1 := config.AvalancheRules(common.Big0, common.Big1)
	require.False(t, rules1.IsPrecompileEnabled(nativeminter.Module.Address))
}

func TestChainConfigMarshalWithUpgrades(t *testing.T) {
	config := ChainConfigWithUpgradesJSON{
		ChainConfig: ChainConfig{
			ChainID:             big.NewInt(1),
			FeeConfig:           DefaultFeeConfig,
			AllowFeeRecipients:  false,
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP150Hash:          common.Hash{},
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
			NetworkUpgrades:     NetworkUpgrades{big.NewInt(0)},
			GenesisPrecompiles:  Precompiles{},
		},
		UpgradeConfig: UpgradeConfig{
			PrecompileUpgrades: []PrecompileUpgrade{
				{
					Config: txallowlist.NewConfig(big.NewInt(100), nil, nil),
				},
			},
		},
	}
	result, err := json.Marshal(&config)
	require.NoError(t, err)
	expectedJSON := `{"chainId":1,"feeConfig":{"gasLimit":8000000,"targetBlockRate":2,"minBaseFee":25000000000,"targetGas":15000000,"baseFeeChangeDenominator":36,"minBlockGasCost":0,"maxBlockGasCost":1000000,"blockGasCostStep":200000},"homesteadBlock":0,"eip150Block":0,"eip150Hash":"0x0000000000000000000000000000000000000000000000000000000000000000","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0,"istanbulBlock":0,"muirGlacierBlock":0,"subnetEVMTimestamp":0,"upgrades":{"precompileUpgrades":[{"txAllowListConfig":{"blockTimestamp":100}}]}}`
	require.JSONEq(t, expectedJSON, string(result))

	var unmarshalled ChainConfigWithUpgradesJSON
	err = json.Unmarshal(result, &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, config, unmarshalled)
}
