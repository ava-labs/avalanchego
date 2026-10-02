// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
)

func TestMain(m *testing.M) {
	RegisterExtras()
	os.Exit(m.Run())
}

func TestSetEthUpgrades(t *testing.T) {
	genesisBlock := big.NewInt(0)
	genesisTimestamp := utils.PointerTo[uint64](0) // extras.Test*Config activate at 0
	tests := []struct {
		name        string
		extraConfig *extras.ChainConfig
		expected    *ChainConfig
	}{
		{
			name:        "launch",
			extraConfig: extras.TestLaunchConfig,
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
			name:        "apricot phase 1",
			extraConfig: extras.TestApricotPhase1Config,
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
			name:        "apricot phase 2",
			extraConfig: extras.TestApricotPhase2Config,
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
			name:        "apricot phase 3",
			extraConfig: extras.TestApricotPhase3Config,
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
			name:        "durango",
			extraConfig: extras.TestDurangoChainConfig,
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
			name:        "etna",
			extraConfig: extras.TestEtnaChainConfig,
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
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			cpy := *test.extraConfig
			actual := WithExtra(
				&ChainConfig{},
				&cpy,
			)
			require.NoError(SetEthUpgrades(actual))

			expected := WithExtra(
				test.expected,
				&cpy,
			)
			require.Equal(expected, actual)
		})
	}
}
