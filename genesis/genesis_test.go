// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "embed"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	utilsJson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

var (
	//go:embed genesis_test.json
	customGenesisConfigJSON  []byte
	invalidGenesisConfigJSON = []byte(`{
		"networkID": 9999}}}}
	}`)

	genesisStakingCfg = &StakingConfig{
		MaxStakeDuration: 365 * 24 * time.Hour,
	}
)

func TestValidateConfig(t *testing.T) {
	tests := map[string]struct {
		networkID uint32
		config    *Config
		err       string
	}{
		"mainnet": {
			networkID: 1,
			config:    &MainnetConfig,
		},
		"fuji": {
			networkID: 5,
			config:    &FujiConfig,
		},
		"local": {
			networkID: 12345,
			config:    &LocalConfig,
		},
		"mainnet (networkID mismatch)": {
			networkID: 2,
			config:    &MainnetConfig,
			err:       "networkID 2 specified but genesis config contains networkID 1",
		},
		"invalid start time": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.StartTime = 999999999999999
				return &thisConfig
			}(),
			err: "start time cannot be in the future",
		},
		"no initial supply": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Allocations = []Allocation{}
				return &thisConfig
			}(),
			err: errNoSupply.Error(),
		},
		"no initial stakers": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakers = []Staker{}
				return &thisConfig
			}(),
			err: errNoStakers.Error(),
		},
		"invalid initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = 0
				return &thisConfig
			}(),
			err: errNoStakeDuration.Error(),
		},
		"too large initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = uint64(genesisStakingCfg.MaxStakeDuration+time.Second) / uint64(time.Second)
				return &thisConfig
			}(),
			err: errStakeDurationTooHigh.Error(),
		},
		"invalid stake offset": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDurationOffset = 100000000
				return &thisConfig
			}(),
			err: "initial stake duration is 31536000 but need at least 400000000 with offset of 100000000",
		},
		"empty initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = []ids.ShortID(nil)
				return &thisConfig
			}(),
			err: errNoInitiallyStakedFunds.Error(),
		},
		"duplicate initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, thisConfig.InitialStakedFunds[0])
				return &thisConfig
			}(),
			err: "duplicated in initial staked funds",
		},
		"initial staked funds not in allocations": {
			networkID: 5,
			config: func() *Config {
				thisConfig := FujiConfig
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, LocalConfig.InitialStakedFunds[0])
				return &thisConfig
			}(),
			err: "does not have an allocation to stake",
		},
		"empty C-Chain genesis": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.CChainGenesis = ""
				return &thisConfig
			}(),
			err: errNoCChainGenesis.Error(),
		},
		"empty message": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Message = ""
				return &thisConfig
			}(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			err := validateConfig(test.networkID, test.config, genesisStakingCfg)
			if len(test.err) > 0 {
				require.Error(err)
				require.Contains(err.Error(), test.err)
				return
			}
			require.NoError(err)
		})
	}
}

func TestGenesisFromFile(t *testing.T) {
	tests := map[string]struct {
		networkID       uint32
		customConfig    []byte
		missingFilepath string
		err             string
		expected        string
	}{
		"mainnet": {
			networkID:    constants.MainnetID,
			customConfig: customGenesisConfigJSON,
			err:          "cannot override genesis config for standard network mainnet (1)",
		},
		"fuji": {
			networkID:    constants.FujiID,
			customConfig: customGenesisConfigJSON,
			err:          "cannot override genesis config for standard network fuji (5)",
		},
		"fuji (with custom specified)": {
			networkID:    constants.FujiID,
			customConfig: localGenesisConfigJSON, // won't load
			err:          "cannot override genesis config for standard network fuji (5)",
		},
		"local": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			err:          "cannot override genesis config for standard network local (12345)",
		},
		"local (with custom specified)": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			err:          "cannot override genesis config for standard network local (12345)",
		},
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expected:     "a1d1838586db85fe94ab1143560c3356df9ba2445794b796bba050be89f4fcb4",
		},
		"custom (networkID mismatch)": {
			networkID:    9999,
			customConfig: localGenesisConfigJSON,
			err:          "networkID 9999 specified but genesis config contains networkID 12345",
		},
		"custom (invalid format)": {
			networkID:    9999,
			customConfig: invalidGenesisConfigJSON,
			err:          "unable to load provided genesis config",
		},
		"custom (missing filepath)": {
			networkID:       9999,
			missingFilepath: "missing.json",
			err:             "unable to load provided genesis config",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// test loading of genesis from file

			require := require.New(t)
			var customFile string
			if len(test.customConfig) > 0 {
				customFile = filepath.Join(t.TempDir(), "config.json")
				require.NoError(perms.WriteFile(customFile, test.customConfig, perms.ReadWrite))
			}

			if len(test.missingFilepath) > 0 {
				customFile = test.missingFilepath
			}

			genesisBytes, _, err := FromFile(test.networkID, customFile, genesisStakingCfg)
			if len(test.err) > 0 {
				require.Error(err)
				require.Contains(err.Error(), test.err)
				return
			}
			require.NoError(err)

			genesisHash := fmt.Sprintf("%x", hashing.ComputeHash256(genesisBytes))
			require.Equal(test.expected, genesisHash, "genesis hash mismatch")

			_, err = genesis.Parse(genesisBytes)
			require.NoError(err)
		})
	}
}

func TestGenesisFromFlag(t *testing.T) {
	tests := map[string]struct {
		networkID    uint32
		customConfig []byte
		err          string
		expected     string
	}{
		// "mainnet": {
		// 	networkID: constants.MainnetID,
		// 	err:       "cannot override genesis config for standard network mainnet (1)",
		// },
		// "fuji": {
		// 	networkID: constants.FujiID,
		// 	err:       "cannot override genesis config for standard network fuji (5)",
		// },
		// "local": {
		// 	networkID: constants.LocalID,
		// 	err:       "cannot override genesis config for standard network local (12345)",
		// },
		// "local (with custom specified)": {
		// 	networkID:    constants.LocalID,
		// 	customConfig: customGenesisConfigJSON,
		// 	err:          "cannot override genesis config for standard network local (12345)",
		// },
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expected:     "a1d1838586db85fe94ab1143560c3356df9ba2445794b796bba050be89f4fcb4",
		},
		// "custom (networkID mismatch)": {
		// 	networkID:    9999,
		// 	customConfig: localGenesisConfigJSON,
		// 	err:          "networkID 9999 specified but genesis config contains networkID 12345",
		// },
		// "custom (invalid format)": {
		// 	networkID:    9999,
		// 	customConfig: invalidGenesisConfigJSON,
		// 	err:          "unable to load genesis content from flag",
		// },
		// "custom (missing content)": {
		// 	networkID: 9999,
		// 	err:       "unable to load genesis content from flag",
		// },
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// test loading of genesis content from flag/env-var

			require := require.New(t)
			var genBytes []byte
			if len(test.customConfig) == 0 {
				// try loading a default config
				var err error
				switch test.networkID {
				case constants.MainnetID:
					genBytes, err = json.Marshal(&MainnetConfig)
					require.NoError(err)
				case constants.TestnetID:
					genBytes, err = json.Marshal(&FujiConfig)
					require.NoError(err)
				case constants.LocalID:
					genBytes, err = json.Marshal(&LocalConfig)
					require.NoError(err)
				default:
					genBytes = make([]byte, 0)
				}
			} else {
				genBytes = test.customConfig
			}
			content := base64.StdEncoding.EncodeToString(genBytes)

			genesisBytes, _, err := FromFlag(test.networkID, content, genesisStakingCfg)
			if len(test.err) > 0 {
				require.Error(err)
				require.Contains(err.Error(), test.err)
				return
			}
			require.NoError(err)

			genesisHash := fmt.Sprintf("%x", hashing.ComputeHash256(genesisBytes))
			require.Equal(test.expected, genesisHash, "genesis hash mismatch")

			_, err = genesis.Parse(genesisBytes)
			require.NoError(err)
		})
	}
}

func TestGenesis(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		// {
		// 	networkID:  constants.MainnetID,
		// 	expectedID: "UUvXi6j7QhVvgpbKM89MP5HdrxKm9CaJeHc187TsDNf8nZdLk",
		// },
		// {
		// 	networkID:  constants.FujiID,
		// 	expectedID: "MSj6o9TpezwsQx4Tv7SHqpVvCbJ8of1ikjsqPZ1bKRjc9zBy3",
		// },
		{
			networkID:  constants.LocalID,
			expectedID: "hBbtNFKLpjuKti32L5bnfZ2vspABkN268t8FincYhQWnWLHxw",
		},
	}
	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			genesisBytes, _, avmArgs, avmReply, platformvmArgs, _ /* platformvmReply,*/, err := FromConfig(config)
			require.NoError(err)

			resAvmArg := &avm.BuildGenesisArgs{
				NetworkID: 12345,
				GenesisData: map[string]avm.AssetDefinition{
					"AVAX": {
						Name:         "Avalanche",
						Symbol:       "AVAX",
						Denomination: 9,
						InitialState: map[string]([]interface{}){
							"fixedCap": {
								avm.Holder{
									Amount:  10000000000000000,
									Address: "local1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmggalnu",
								},
								avm.Holder{
									Amount:  300000000000000000,
									Address: "local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
								},
							},
						},
						Memo: "0xb3d82b1367d362de99ab59a658165aff520cbd4db3d82b1367d362de99ab59a658165aff520cbd4d6d119826",
					},
				},
			}
			require.Equal(avmArgs, resAvmArg)

			resAvmReply := &avm.BuildGenesisReply{
				Bytes:    "0x000000000001000441564158000030390000000000000000000000000000000000000000000000000000000000000000000000000000000000000028b3d82b1367d362de99ab59a658165aff520cbd4db3d82b1367d362de99ab59a658165aff520cbd4d00094176616c616e6368650004415641580900000001000000000000000200000007002386f26fc1000000000000000000000000000100000001e0cfe8cae22827d032805ded484e393ce51cbedb000000070429d069189e0000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c25c5e815",
				Encoding: 0x0,
			}
			require.Equal(avmReply, resAvmReply)

			var (
				exactDelegationFee0 = utilsJson.Uint32(1000000)
				exactDelegationFee1 = utilsJson.Uint32(500000)
				exactDelegationFee3 = utilsJson.Uint32(250000)
				exactDelegationFee4 = utilsJson.Uint32(125000)
				exactDelegationFee5 = utilsJson.Uint32(62500)
			)
			resPlatformReply := &api.BuildGenesisArgs{
				AvaxAssetID: ids.ID{0xdb, 0xcf, 0x89, 0xf, 0x77, 0xf4, 0x9b, 0x96, 0x85, 0x76, 0x48, 0xb7, 0x2b, 0x77, 0xf9, 0xf8, 0x29, 0x37, 0xf2, 0x8a, 0x68, 0x70, 0x4a, 0xf0, 0x5d, 0xa0, 0xdc, 0x12, 0xba, 0x53, 0xf2, 0xdb},
				NetworkID:   0x3039,
				UTXOs: []api.UTXO{
					{
						Locktime: 0x0,
						Amount:   0x470de4df820000,
						Address:  "local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
						Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
					},
					{
						Locktime: 0x61622d00,
						Amount:   0x2386f26fc10000,
						Address:  "local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
						Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
					},
					{
						Locktime: 0x61622d00,
						Amount:   0x2386f26fc10000,
						Address:  "local1ur873jhz9qnaqv5qthk5sn3e8nj3e0kmggalnu",
						Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
					},
				},
				Validators: []api.PermissionlessValidator{
					{
						Staker: api.Staker{
							TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							StartTime:   0x62f9c4c0,
							EndTime:     0x64daf840,
							Weight:      0x0,
							NodeID:      ids.NodeID{0x47, 0x9f, 0x66, 0xc8, 0xbe, 0x89, 0x58, 0x30, 0x54, 0x7e, 0x70, 0xb4, 0xb2, 0x98, 0xca, 0xfd, 0x43, 0x3d, 0xba, 0x6e},
							StakeAmount: nil,
						},
						RewardOwner: &api.Owner{
							Locktime:  0,
							Threshold: 1,
							Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
						},
						ValidationRewardOwner:  nil,
						DelegationRewardOwner:  nil,
						PotentialReward:        nil,
						AccruedDelegateeReward: nil,
						DelegationFee:          0,
						ExactDelegationFee:     &exactDelegationFee0,
						Uptime:                 nil,
						Connected:              false,
						Staked: []api.UTXO{
							{
								Locktime: 0x61622d00,
								Amount:   0x71afd498d0000,
								Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
								Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
							},
						},
						Signer:          nil,
						DelegatorCount:  nil,
						DelegatorWeight: nil,
						Delegators:      nil,
					},
					{
						Staker: api.Staker{
							TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							StartTime:   0x62f9c4c0,
							EndTime:     0x64dae328,
							Weight:      0x0,
							NodeID:      ids.NodeID{0xde, 0x31, 0xb4, 0xd8, 0xb2, 0x29, 0x91, 0xd5, 0x1a, 0xa6, 0xaa, 0x1f, 0xc7, 0x33, 0xf2, 0x3a, 0x85, 0x1a, 0x8c, 0x94},
							StakeAmount: nil,
						},
						RewardOwner: &api.Owner{
							Locktime:  0,
							Threshold: 1,
							Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
						},
						ValidationRewardOwner:  nil,
						DelegationRewardOwner:  nil,
						PotentialReward:        nil,
						AccruedDelegateeReward: nil,
						DelegationFee:          0,
						ExactDelegationFee:     &exactDelegationFee1,
						Uptime:                 nil,
						Connected:              false,
						Staked: []api.UTXO{
							{
								Locktime: 0x61622d00,
								Amount:   0x71afd498d0000,
								Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
								Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
							},
						},
						Signer:          nil,
						DelegatorCount:  nil,
						DelegatorWeight: nil,
						Delegators:      nil,
					},
					{
						Staker: api.Staker{
							TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							StartTime:   0x62f9c4c0,
							EndTime:     0x64dace10,
							Weight:      0x0,
							NodeID:      ids.NodeID{0xe9, 0x9, 0x4f, 0x73, 0x69, 0x80, 0x2, 0xfd, 0x52, 0xc9, 0x8, 0x19, 0xb4, 0x57, 0xb9, 0xfb, 0xc8, 0x66, 0xab, 0x80},
							StakeAmount: nil,
						},
						RewardOwner: &api.Owner{
							Locktime:  0,
							Threshold: 1,
							Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
						},

						ValidationRewardOwner:  nil,
						DelegationRewardOwner:  nil,
						PotentialReward:        nil,
						AccruedDelegateeReward: nil,
						DelegationFee:          0,
						ExactDelegationFee:     &exactDelegationFee3,
						Uptime:                 nil,
						Connected:              false,
						Staked: []api.UTXO{
							{
								Locktime: 0x61622d00,
								Amount:   0x71afd498d0000,
								Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
								Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
							},
						},
						Signer:          nil,
						DelegatorCount:  nil,
						DelegatorWeight: nil,
						Delegators:      nil,
					},
					{
						Staker: api.Staker{
							TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							StartTime:   0x62f9c4c0,
							EndTime:     0x64dab8f8,
							Weight:      0x0,
							NodeID:      ids.NodeID{0xaa, 0x18, 0xd3, 0x99, 0x1c, 0xf6, 0x37, 0xaa, 0x6c, 0x16, 0x2f, 0x5e, 0x95, 0xcf, 0x16, 0x3f, 0x69, 0xcd, 0x82, 0x91},
							StakeAmount: nil,
						},
						RewardOwner: &api.Owner{
							Locktime:  0,
							Threshold: 1,
							Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
						},
						ValidationRewardOwner:  nil,
						DelegationRewardOwner:  nil,
						PotentialReward:        nil,
						AccruedDelegateeReward: nil,
						DelegationFee:          0,
						ExactDelegationFee:     &exactDelegationFee4,
						Uptime:                 nil,
						Connected:              false,
						Staked: []api.UTXO{
							{
								Locktime: 0x61622d00,
								Amount:   0x71afd498d0000,
								Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
								Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
							},
						},
						Signer:          nil,
						DelegatorCount:  nil,
						DelegatorWeight: nil,
						Delegators:      nil,
					},
					{
						Staker: api.Staker{
							TxID:        ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							StartTime:   0x62f9c4c0,
							EndTime:     0x64daa3e0,
							Weight:      0x0,
							NodeID:      ids.NodeID{0xf2, 0x9b, 0xce, 0x5f, 0x34, 0xa7, 0x43, 0x1, 0xeb, 0xd, 0xe7, 0x16, 0xd5, 0x19, 0x4e, 0x4a, 0x4a, 0xea, 0x5d, 0x7a},
							StakeAmount: nil,
						},
						RewardOwner: &api.Owner{
							Locktime:  0,
							Threshold: 1,
							Addresses: []string{"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"},
						},
						ValidationRewardOwner:  nil,
						DelegationRewardOwner:  nil,
						PotentialReward:        nil,
						AccruedDelegateeReward: nil,
						DelegationFee:          0,
						ExactDelegationFee:     &exactDelegationFee5,
						Uptime:                 nil,
						Connected:              false,
						Staked: []api.UTXO{
							{
								Locktime: 0x61622d00,
								Amount:   0x71afd498d0000,
								Address:  "local1g65uqn6t77p656w64023nh8nd9updzmxyymev2",
								Message:  "0xb3d82b1367d362de99ab59a658165aff520cbd4d4d5779cb",
							},
						},
						Signer:          nil,
						DelegatorCount:  nil,
						DelegatorWeight: nil,
						Delegators:      nil,
					},
				},
				Chains: []api.Chain{
					{
						GenesisData: "0x000000000001000441564158000030390000000000000000000000000000000000000000000000000000000000000000000000000000000000000028b3d82b1367d362de99ab59a658165aff520cbd4db3d82b1367d362de99ab59a658165aff520cbd4d00094176616c616e6368650004415641580900000001000000000000000200000007002386f26fc1000000000000000000000000000100000001e0cfe8cae22827d032805ded484e393ce51cbedb000000070429d069189e0000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c25c5e815",
						VMID:        ids.ID{0x61, 0x76, 0x6d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						FxIDs: []ids.ID{
							{0x73, 0x65, 0x63, 0x70, 0x32, 0x35, 0x36, 0x6b, 0x31, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							{0x6e, 0x66, 0x74, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
							{0x70, 0x72, 0x6f, 0x70, 0x65, 0x72, 0x74, 0x79, 0x66, 0x78, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						},
						Name:     "X-Chain",
						SubnetID: ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					},
					{
						GenesisData: "0x7b22636f6e666967223a7b22636861696e4964223a34333131322c22686f6d657374656164426c6f636b223a302c2264616f466f726b426c6f636b223a302c2264616f466f726b537570706f7274223a747275652c22656970313530426c6f636b223a302c2265697031353048617368223a22307832303836373939616565626561653133356332343663363530323163383262346531356132633435313334303939336161636664323735313838363531346630222c22656970313535426c6f636b223a302c22656970313538426c6f636b223a302c2262797a616e7469756d426c6f636b223a302c22636f6e7374616e74696e6f706c65426c6f636b223a302c2270657465727362757267426c6f636b223a302c22697374616e62756c426c6f636b223a302c226d756972476c6163696572426c6f636b223a302c2261707269636f74506861736531426c6f636b54696d657374616d70223a302c2261707269636f74506861736532426c6f636b54696d657374616d70223a307d2c226e6f6e6365223a22307830222c2274696d657374616d70223a22307830222c22657874726144617461223a2230783030222c226761734c696d6974223a22307835663565313030222c22646966666963756c7479223a22307830222c226d697848617368223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030222c22636f696e62617365223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030222c22616c6c6f63223a7b2238646239374337634563453234396332623938624443303232364363344332413537424635324643223a7b2262616c616e6365223a22307832393542453936453634303636393732303030303030227d7d2c226e756d626572223a22307830222c2267617355736564223a22307830222c22706172656e7448617368223a22307830303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030227d8d7c4cd3",
						VMID:        ids.ID{0x65, 0x76, 0x6d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						FxIDs:       nil,
						Name:        "C-Chain",
						SubnetID:    ids.ID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
					},
				},
				Time:          0x62f9c4c0,
				InitialSupply: 0x4fefa17b7240000,
				Message:       "{{ fun_quote }}",
				Encoding:      0x0,
			}

			require.Equal(platformvmArgs, resPlatformReply)

			var genesisID ids.ID = hashing.ComputeHash256Array(genesisBytes)
			require.Equal(test.expectedID, genesisID.String())
		})
	}
}

func TestVMGenesis(t *testing.T) {
	type vmTest struct {
		vmID       ids.ID
		expectedID string
	}
	tests := []struct {
		networkID uint32
		vmTest    []vmTest
	}{
		{
			networkID: constants.MainnetID,
			vmTest: []vmTest{
				{
					vmID:       constants.AVMID,
					expectedID: "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
				},
			},
		},
		{
			networkID: constants.FujiID,
			vmTest: []vmTest{
				{
					vmID:       constants.AVMID,
					expectedID: "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp",
				},
			},
		},
		{
			networkID: constants.LocalID,
			vmTest: []vmTest{
				{
					vmID:       constants.AVMID,
					expectedID: "2eNy1mUFdmaxXNj1eQHUe7Np4gju9sJsEtWQ4MX3ToiNKuADed",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "2CA6j5zYzasynPsFeNoqWkmTCt3VScMvXUZHbfDJ8k3oGzAPtU",
				},
			},
		},
	}

	for _, test := range tests {
		for _, vmTest := range test.vmTest {
			name := fmt.Sprintf("%s-%s",
				constants.NetworkIDToNetworkName[test.networkID],
				vmTest.vmID,
			)
			t.Run(name, func(t *testing.T) {
				require := require.New(t)

				config := GetConfig(test.networkID)
				genesisBytes, _, _, _, _, _, err := FromConfig(config)
				require.NoError(err)

				genesisTx, err := VMGenesis(genesisBytes, vmTest.vmID)
				require.NoError(err)

				require.Equal(
					vmTest.expectedID,
					genesisTx.ID().String(),
					"%s genesisID with networkID %d mismatch",
					vmTest.vmID,
					test.networkID,
				)
			})
		}
	}
}

func TestAVAXAssetID(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  constants.MainnetID,
			expectedID: "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
		},
		{
			networkID:  constants.FujiID,
			expectedID: "U8iRqJoiJm8xZHAacmvYyZVwqQx6uDNtQeP3CQ6fcgQk3JqnK",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "2fombhL7aGPwj3KH4bfrmJwW6PVnMobf9Y2fn9GwxiAAJyFDbe",
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			_, avaxAssetID, _, _, _, _, err := FromConfig(config)
			require.NoError(err)

			require.Equal(
				test.expectedID,
				avaxAssetID.String(),
				"AVAX assetID with networkID %d mismatch",
				test.networkID,
			)
		})
	}
}
