// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
)

func TestMain(m *testing.M) {
	RegisterExtras()
	os.Exit(m.Run())
}

func TestSetEthUpgrades(t *testing.T) {
	genesisBlock := big.NewInt(0)
	genesisTimestamp := utils.PointerTo(initiallyActive)
	tests := []struct {
		fork     upgradetest.Fork
		expected *ChainConfig
	}{
		{
			fork: upgradetest.NoUpgrades,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         nil,
				LondonBlock:         nil,
				ShanghaiTime:        nil,
				CancunTime:          nil,
			},
		},
		{
			fork: upgradetest.ApricotPhase1,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         nil,
				LondonBlock:         nil,
				ShanghaiTime:        nil,
				CancunTime:          nil,
			},
		},
		{
			fork: upgradetest.ApricotPhase2,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         nil,
				ShanghaiTime:        nil,
				CancunTime:          nil,
			},
		},
		{
			fork: upgradetest.ApricotPhase3,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
				ShanghaiTime:        nil,
				CancunTime:          nil,
			},
		},
		{
			fork: upgradetest.Durango,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
				ShanghaiTime:        genesisTimestamp,
				CancunTime:          nil,
			},
		},
		{
			fork: upgradetest.Etna,
			expected: &ChainConfig{
				HomesteadBlock:      genesisBlock,
				DAOForkBlock:        genesisBlock,
				DAOForkSupport:      true,
				EIP150Block:         genesisBlock,
				EIP155Block:         genesisBlock,
				EIP158Block:         genesisBlock,
				ByzantiumBlock:      genesisBlock,
				ConstantinopleBlock: genesisBlock,
				PetersburgBlock:     genesisBlock,
				IstanbulBlock:       genesisBlock,
				MuirGlacierBlock:    genesisBlock,
				BerlinBlock:         genesisBlock,
				LondonBlock:         genesisBlock,
				ShanghaiTime:        genesisTimestamp,
				CancunTime:          genesisTimestamp,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.fork.String(), func(t *testing.T) {
			require := require.New(t)

			extraConfig := &extras.ChainConfig{
				NetworkUpgrades: extras.GetNetworkUpgrades(upgradetest.GetConfig(test.fork)),
			}
			actual := WithExtra(
				&ChainConfig{},
				extraConfig,
			)
			require.NoError(SetEthUpgrades(actual))

			expected := WithExtra(
				test.expected,
				extraConfig,
			)
			require.Equal(expected, actual)
		})
	}
}
