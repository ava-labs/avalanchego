// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MetalBlockchain/libevm/core"
	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/upgrade"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/hashing"
	"github.com/MetalBlockchain/metalgo/utils/perms"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/genesis"
)

var (
	//go:embed genesis_test.json
	customGenesisConfigJSON  []byte
	invalidGenesisConfigJSON = []byte(`{
		"networkID": 9999}}}}
	}`)

	//go:embed genesis_test_invalid_allocations.json
	customGenesisConfigInvalidAllocationsJSON []byte

	genesisStakingCfg = &StakingConfig{
		MaxStakeDuration: 365 * 24 * time.Hour,
	}
)

func TestValidateConfig(t *testing.T) {
	tests := map[string]struct {
		networkID   uint32
		config      *Config
		expectedErr error
	}{
		"mainnet": {
			networkID:   1,
			config:      &MainnetConfig,
			expectedErr: nil,
		},
		"tahoe": {
			networkID:   5,
			config:      &TahoeConfig,
			expectedErr: nil,
		},
		"local": {
			networkID:   12345,
			config:      &LocalConfig,
			expectedErr: nil,
		},
		"mainnet (networkID mismatch)": {
			networkID:   2,
			config:      &MainnetConfig,
			expectedErr: errConflictingNetworkIDs,
		},
		"invalid start time": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.StartTime = 999999999999999
				return &thisConfig
			}(),
			expectedErr: errFutureStartTime,
		},
		"no initial supply": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Allocations = []Allocation{}
				return &thisConfig
			}(),
			expectedErr: errNoSupply,
		},
		"no initial stakers": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakers = []Staker{}
				return &thisConfig
			}(),
			expectedErr: errNoStakers,
		},
		"invalid initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = 0
				return &thisConfig
			}(),
			expectedErr: errNoStakeDuration,
		},
		"too large initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = uint64(genesisStakingCfg.MaxStakeDuration+time.Second) / uint64(time.Second)
				return &thisConfig
			}(),
			expectedErr: errStakeDurationTooHigh,
		},
		"invalid stake offset": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDurationOffset = 100000000
				return &thisConfig
			}(),
			expectedErr: errInitialStakeDurationTooLow,
		},
		"empty initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = []ids.ShortID(nil)
				return &thisConfig
			}(),
			expectedErr: errNoInitiallyStakedFunds,
		},
		"duplicate initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, thisConfig.InitialStakedFunds[0])
				return &thisConfig
			}(),
			expectedErr: errDuplicateInitiallyStakedAddress,
		},
		"initial staked funds not in allocations": {
			networkID: 5,
			config: func() *Config {
				thisConfig := TahoeConfig
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, LocalConfig.InitialStakedFunds[0])
				return &thisConfig
			}(),
			expectedErr: errNoAllocationToStake,
		},
		"empty C-Chain genesis": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.CChainGenesis = ""
				return &thisConfig
			}(),
			expectedErr: errNoCChainGenesis,
		},
		"empty message": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Message = ""
				return &thisConfig
			}(),
			expectedErr: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateConfig(test.networkID, test.config, genesisStakingCfg)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestGenesisFromFile(t *testing.T) {
	tests := map[string]struct {
		networkID       uint32
		customConfig    []byte
		missingFilepath string
		expectedErr     error
		expectedHash    string
	}{
		"mainnet": {
			networkID:    constants.MainnetID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"testnet": {
			networkID:    constants.TahoeID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"testnet (with custom specified)": {
			networkID:    constants.TahoeID,
			customConfig: localGenesisConfigJSON, // won't load
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"local": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"local (with custom specified)": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expectedErr:  nil,
			expectedHash: "18dc0a7ab9b257b86ee65870649a0dabbb76c03d0ca9f5c44a43ce0d0a7f20c6",
		},
		"custom (networkID mismatch)": {
			networkID:    9999,
			customConfig: localGenesisConfigJSON,
			expectedErr:  errConflictingNetworkIDs,
		},
		"custom (invalid format)": {
			networkID:    9999,
			customConfig: invalidGenesisConfigJSON,
			expectedErr:  errInvalidGenesisJSON,
		},
		"custom (missing filepath)": {
			networkID:       9999,
			missingFilepath: "missing.json",
			expectedErr:     os.ErrNotExist,
		},
		"custom (locked allocations amount too low)": {
			networkID:    9999,
			customConfig: customGenesisConfigInvalidAllocationsJSON,
			expectedErr:  errAllocationsLockedAmountTooLow,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// test loading of genesis from file
			var customFile string
			if len(test.customConfig) > 0 {
				customFile = filepath.Join(t.TempDir(), "config.json")
				require.NoError(perms.WriteFile(customFile, test.customConfig, perms.ReadWrite))
			}

			if len(test.missingFilepath) > 0 {
				customFile = test.missingFilepath
			}

			genesisBytes, _, err := FromFile(test.networkID, customFile, genesisStakingCfg)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				genesisHash := hex.EncodeToString(hashing.ComputeHash256(genesisBytes))
				require.Equal(test.expectedHash, genesisHash, "genesis hash mismatch")

				_, err = genesis.Parse(genesisBytes)
				require.NoError(err)
			}
		})
	}
}

func TestGenesisFromFlag(t *testing.T) {
	tests := map[string]struct {
		networkID    uint32
		customConfig []byte
		expectedErr  error
		expectedHash string
	}{
		"mainnet": {
			networkID:   constants.MainnetID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"tahoe": {
			networkID:   constants.TahoeID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"local": {
			networkID:   constants.LocalID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"local (with custom specified)": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expectedErr:  nil,
			expectedHash: "18dc0a7ab9b257b86ee65870649a0dabbb76c03d0ca9f5c44a43ce0d0a7f20c6",
		},
		"custom (networkID mismatch)": {
			networkID:    9999,
			customConfig: localGenesisConfigJSON,
			expectedErr:  errConflictingNetworkIDs,
		},
		"custom (invalid format)": {
			networkID:    9999,
			customConfig: invalidGenesisConfigJSON,
			expectedErr:  errInvalidGenesisJSON,
		},
		"custom (missing content)": {
			networkID:   9999,
			expectedErr: errInvalidGenesisJSON,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// test loading of genesis content from flag/env-var
			var genBytes []byte
			if len(test.customConfig) == 0 {
				// try loading a default config
				var err error
				switch test.networkID {
				case constants.MainnetID:
					genBytes, err = json.Marshal(&MainnetConfig)
					require.NoError(err)
				case constants.TahoeID:
					genBytes, err = json.Marshal(&TahoeConfig)
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
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				genesisHash := hex.EncodeToString(hashing.ComputeHash256(genesisBytes))
				require.Equal(test.expectedHash, genesisHash, "genesis hash mismatch")

				_, err = genesis.Parse(genesisBytes)
				require.NoError(err)
			}
		})
	}
}

func TestGenesis(t *testing.T) {
	tests := []struct {
		config     *Config
		expectedID string
	}{
		{
			config:     &MainnetConfig,
			expectedID: "2QWBywoaRDBk7Z1YmnGm4W2Mid5CUqBHGWdiHg45uRTJTQ8TDD",
		},
		{
			config:     &TahoeConfig,
			expectedID: "tZW9eSc1k8bMxQusjYSVucoJQhuRaWimezf5feGQFxiPgk85q",
		},
		{
			config:     &unmodifiedLocalConfig,
			expectedID: "cJ5P2MJoE87LFevQKmWVLbE88F7oSDNWbf6ZDWN9PrzeHP8te",
		},
	}
	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.config.NetworkID], func(t *testing.T) {
			require := require.New(t)

			genesisBytes, _, err := FromConfig(test.config)
			require.NoError(err)

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
					expectedID: "UQg9hfKuviMwwkR16hE8nHyrmG6f5tax5seEoqUSiBmsTghXE",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "28fJD1hMz2PSRJKJt7YT41urTPR37rUNUcdeJ8daoiwP6DGnAR",
				},
			},
		},
		{
			networkID: constants.TahoeID,
			vmTest: []vmTest{
				{
					vmID:       constants.AVMID,
					expectedID: "N8BzztcRDHj6nNcGLbdimm6FSwE34rSVSgxhcV18TAaYSa4Q8",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "t34kbq3fgdNaurCHn4aJpayuE46vh5AozKPZZG6MrjE2F7XP6",
				},
			},
		},
		{
			networkID: constants.LocalID,
			vmTest: []vmTest{
				{
					vmID:       constants.AVMID,
					expectedID: "2EwQEpAoKi3b8GgAhKGprkGiJYFduE9bbbEANMWioSWXsb6ZQz",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "2owdGqyG6FFzTHy5qhenDXQcEghvr571KZE3gSfRJERSJinuwC",
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
				genesisBytes, _, err := FromConfig(config)
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
			expectedID: "2pjq58dnYTfrUJvvnC1uHDBP87DyP2oJj9uTmt3vdJg9Nhr9d4",
		},
		{
			networkID:  constants.TahoeID,
			expectedID: "2QpCJwPk3nzi1VqJEuaFA44WM2UUzraBXQyH6jMGLTLQhqoe4n",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "ui6eSQwLELPt3zZsreVkdSFhimBdAyXgADd7CGfQ58NeDNzCS",
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			_, avaxAssetID, err := FromConfig(config)
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

func TestCChainGenesisTimestamp(t *testing.T) {
	tests := []struct {
		networkID           uint32
		expectedGenesisTime uint64
	}{
		{
			networkID:           constants.MainnetID,
			expectedGenesisTime: 0,
		},
		{
			networkID:           constants.TahoeID,
			expectedGenesisTime: 0,
		},
		{
			networkID:           constants.LocalID,
			expectedGenesisTime: uint64(upgrade.InitiallyActiveTime.Unix()),
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			var cChainGenesis core.Genesis
			require.NoError(json.Unmarshal([]byte(config.CChainGenesis), &cChainGenesis))
			require.Equal(
				test.expectedGenesisTime,
				cChainGenesis.Timestamp,
				"C-Chain genesis time with networkID %d mismatch",
				test.networkID,
			)
		})
	}
}
