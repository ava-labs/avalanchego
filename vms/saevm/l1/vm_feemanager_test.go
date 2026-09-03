// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"

	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

// TestFeeManagerTransitionAtHelicon drives the SAE end-to-end:
// genesis-active feeManager with admin role + non-zero InitialFeeConfig,
// build a block past Helicon, assert storage at 0x02..03 is wiped and
// the precompile is no longer reported as enabled.
func TestFeeManagerForceDisabledAtHeliconActivation(t *testing.T) {
	const adminIdx = 0
	now := postHeliconStartTime(t)

	initialFeeConfig := commontype.FeeConfig{
		GasLimit:                 big.NewInt(8_000_000),
		TargetBlockRate:          2,
		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),
		MinBlockGasCost:          big.NewInt(0),
		MaxBlockGasCost:          big.NewInt(1_000_000),
		BlockGasCostStep:         big.NewInt(200_000),
	}

	sut := newSUT(
		t,
		withFork(upgradetest.Helicon),
		withNumAccounts(2),
		withNow(now),
		withGenesisConfig(func(genesis *core.Genesis, addresses []common.Address) {
			legacyparams.GetExtra(genesis.Config).GenesisPrecompiles = extras.Precompiles{
				feemanager.ConfigKey: feemanager.NewConfig(
					utils.PointerTo[uint64](0),
					[]common.Address{addresses[adminIdx]},
					nil, nil,
					&initialFeeConfig,
				),
			}
		}),
	)

	admin := sut.ethWallet.Addresses()[adminIdx]

	// Pre-Helicon: genesis state has the admin role + stored fee config.
	require.Equal(t, allowlist.AdminRole,
		feeManagerStatusAt(t, sut, admin, rpc.LatestBlockNumber),
		"admin role must be present in genesis state")
	require.Equal(t, initialFeeConfig,
		feeManagerStoredFeeConfigAt(t, sut, rpc.LatestBlockNumber),
		"stored fee config must reflect InitialFeeConfig at block 0")

	// Build a block past Helicon. Activator fires the synthetic Disable
	// upgrade, SelfDestruct + Finalise wipe 0x02..03.
	settleTx := sut.sendTransferTx(t, adminIdx, 1, common.Big1)
	block := sut.buildAcceptExecuteBlock(t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())

	// Post-Helicon: storage wiped + chain config no longer reports
	// feeManager as enabled.
	require.False(t, sut.isPrecompileEnabledAtLatest(feemanager.ContractAddress),
		"feeManager must be disabled per chain config after Helicon")
	require.Equal(t, allowlist.NoRole,
		feeManagerStatusAt(t, sut, admin, rpc.LatestBlockNumber),
		"admin role must be cleared after the Helicon disable")
	require.Equal(t, zeroFeeConfig(),
		feeManagerStoredFeeConfigAt(t, sut, rpc.LatestBlockNumber),
		"stored fee config must be zeroed after SelfDestruct")
}

// feeManagerStatusAt reads the feeManager allowlist role for `address`
// against the StateDB rooted at `blockNumber`.
func feeManagerStatusAt(t *testing.T, sut *SUT, address common.Address, blockNumber rpc.BlockNumber) allowlist.Role {
	t.Helper()
	stateDB, _, err := sut.vm.GethRPCBackends().StateAndHeaderByNumber(sut.ctx, blockNumber)
	require.NoError(t, err)
	return feemanager.GetFeeManagerStatus(stateDB, address)
}

func feeManagerStoredFeeConfigAt(t *testing.T, sut *SUT, blockNumber rpc.BlockNumber) commontype.FeeConfig {
	t.Helper()
	stateDB, _, err := sut.vm.GethRPCBackends().StateAndHeaderByNumber(sut.ctx, blockNumber)
	require.NoError(t, err)
	return feemanager.GetStoredFeeConfig(stateDB)
}

func zeroFeeConfig() commontype.FeeConfig {
	return commontype.FeeConfig{
		GasLimit:                 new(big.Int),
		MinBaseFee:               new(big.Int),
		TargetGas:                new(big.Int),
		BaseFeeChangeDenominator: new(big.Int),
		MinBlockGasCost:          new(big.Int),
		MaxBlockGasCost:          new(big.Int),
		BlockGasCostStep:         new(big.Int),
	}
}
