// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/params/paramstest"
	"github.com/ava-labs/subnet-evm/utils"
)

func TestGenesisEthUpgrades(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	preEthUpgrades := params.WithExtra(
		&params.ChainConfig{
			ChainID:        big.NewInt(43114), // Specifically refers to mainnet for this UT
			HomesteadBlock: big.NewInt(0),
			// For this test to be a proper regression test, DAOForkBlock and
			// DAOForkSupport should be set to match the values in
			// [params.SetEthUpgrades]. Otherwise, in case of a regression, the test
			// would pass as there would be a mismatch at genesis, which is
			// incorrectly considered a success.
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
		},
		&extras.ChainConfig{
			FeeConfig: commontype.FeeConfig{
				MinBaseFee: big.NewInt(1),
			},
			NetworkUpgrades: extras.NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
			},
		},
	)
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	config := *preEthUpgrades
	// Set this up once, just to get the genesis hash
	_, genHash, err := SetupGenesisBlock(db, tdb, &Genesis{Config: &config}, common.Hash{}, false)
	require.NoError(t, err)
	// Write the configuration back to the db as it would be in prior versions
	rawdb.WriteChainConfig(db, genHash, preEthUpgrades)
	// Make some other block
	block := types.NewBlock(
		&types.Header{
			Number:     big.NewInt(1640340), // Berlin activation on mainnet
			Difficulty: big.NewInt(1),
			ParentHash: genHash,
			Time:       uint64(time.Now().Unix()),
		},
		nil, nil, nil, nil,
	)
	rawdb.WriteBlock(db, block)
	// We should still be able to re-initialize
	config = *preEthUpgrades
	require.NoError(t, params.SetEthUpgrades(&config)) // New versions will set additional fields eg, LondonBlock
	_, _, err = SetupGenesisBlock(db, tdb, &Genesis{Config: &config}, block.Hash(), false)
	require.NoError(t, err)
}

func TestGenesisToBlockDecoding(t *testing.T) {
	previousHashes := map[upgradetest.Fork]common.Hash{
		upgradetest.ApricotPhase5: common.HexToHash("0x6116de25352c93149542e950162c7305f207bbc17b0eb725136b78c80aed79cc"),
		upgradetest.ApricotPhase6: common.HexToHash("0x74dd5d404823f342fb3d372ea289565e5b1ff25d07e48a59db8130c5f61e941a"),
		upgradetest.Banff:         common.HexToHash("0x74dd5d404823f342fb3d372ea289565e5b1ff25d07e48a59db8130c5f61e941a"),
		upgradetest.Cortina:       common.HexToHash("0x74dd5d404823f342fb3d372ea289565e5b1ff25d07e48a59db8130c5f61e941a"),
		upgradetest.Durango:       common.HexToHash("0x74dd5d404823f342fb3d372ea289565e5b1ff25d07e48a59db8130c5f61e941a"),
		upgradetest.Etna:          common.HexToHash("0xa5de01cb7e5c6d721be62ab4b37878e863d65e0c1fe308e5df1f4c5b148650f9"),
		upgradetest.Fortuna:       common.HexToHash("0xa5de01cb7e5c6d721be62ab4b37878e863d65e0c1fe308e5df1f4c5b148650f9"),
		upgradetest.Granite:       common.HexToHash("0x4d5ff1b8509d30c6b7842b784a6cf8be78e34c78bc5de8559ea6f15ad18d6a10"),
	}
	for fork, chainConfig := range paramstest.ForkToChainConfig {
		t.Run(fork.String(), func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			tdb := triedb.NewDatabase(db, triedb.HashDefaults)
			genesis := &Genesis{
				Config: chainConfig,
			}
			block, err := genesis.Commit(db, tdb)
			require.NoError(t, err)

			readHeader := rawdb.ReadHeader(db, block.Hash(), 0)
			require.Equal(t, block.Hash(), readHeader.Hash())
			require.Equal(t, previousHashes[fork], block.Hash())
			require.EqualValues(t, block.Header(), readHeader)
		})
	}
}
