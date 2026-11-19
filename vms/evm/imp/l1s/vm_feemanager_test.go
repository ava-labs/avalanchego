// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/upgrade"
)

// TestFeeManagerTransitionAtIgloo drives the SAE end-to-end:
// genesis-active feeManager with admin role + non-zero InitialFeeConfig,
// build a block past Igloo, assert storage at 0x02..03 is wiped and
// the precompile is no longer reported as enabled.
func TestFeeManagerForceDisabledAtIglooActivation(t *testing.T) {
	const adminIdx = 0

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

	wallet := newWallet(t, 2)
	admin := wallet.Addresses()[adminIdx]

	// The genesis predates Igloo, which activates at the first block.
	iglooTime := upgrade.InitiallyActiveTime.Add(time.Minute)
	timeOpt, _ := withVMTime(iglooTime)
	ctx, sut := newSUT(
		t,
		withMaxAllocFor(wallet.Addresses()...),
		withIglooTime(iglooTime),
		timeOpt,
		withGenesisPrecompiles(extras.Precompiles{
			feemanager.ConfigKey: feemanager.NewConfig(
				new(uint64(0)),
				[]common.Address{admin},
				nil, nil,
				&initialFeeConfig,
			),
		}),
	)

	// Pre-Igloo: genesis state has the admin role + stored fee config.
	require.Equal(t, allowlist.AdminRole,
		feeManagerStatusAt(ctx, t, sut, admin, rpc.LatestBlockNumber),
		"admin role must be present in genesis state")
	require.Equal(t, initialFeeConfig,
		feeManagerStoredFeeConfigAt(ctx, t, sut, rpc.LatestBlockNumber),
		"stored fee config must reflect InitialFeeConfig at block 0")

	// Build a block past Igloo. Activator fires the synthetic Disable
	// upgrade, SelfDestruct + Finalise wipe 0x02..03.
	settleTx := sut.send(ctx, t, wallet, adminIdx, transferTx(wallet.Addresses()[1], common.Big1))
	block := sut.RunConsensusLoop(ctx, t)
	require.Len(t, block.Transactions(), 1)
	require.Equal(t, settleTx.Hash(), block.Transactions()[0].Hash())

	// Post-Igloo: storage wiped + chain config no longer reports
	// feeManager as enabled.
	require.False(t, sut.isPrecompileEnabledAtLatest(feemanager.ContractAddress),
		"feeManager must be disabled per chain config after Igloo")
	require.Equal(t, allowlist.NoRole,
		feeManagerStatusAt(ctx, t, sut, admin, rpc.LatestBlockNumber),
		"admin role must be cleared after the Igloo disable")
	require.Equal(t, zeroFeeConfig(),
		feeManagerStoredFeeConfigAt(ctx, t, sut, rpc.LatestBlockNumber),
		"stored fee config must be zeroed after SelfDestruct")
}

// feeManagerStatusAt reads the feeManager allowlist role for `address`
// against the StateDB rooted at `blockNumber`.
func feeManagerStatusAt(ctx context.Context, t *testing.T, sut *SUT, address common.Address, blockNumber rpc.BlockNumber) allowlist.Role {
	t.Helper()
	stateDB, _, err := sut.GethRPCBackends().StateAndHeaderByNumber(ctx, blockNumber)
	require.NoError(t, err)
	return feemanager.GetFeeManagerStatus(stateDB, address)
}

func feeManagerStoredFeeConfigAt(ctx context.Context, t *testing.T, sut *SUT, blockNumber rpc.BlockNumber) commontype.FeeConfig {
	t.Helper()
	stateDB, _, err := sut.GethRPCBackends().StateAndHeaderByNumber(ctx, blockNumber)
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
