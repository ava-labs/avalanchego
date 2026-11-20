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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
)

// TestFeeManagerDisabledAtIgloo asserts that the fee manager MUST be disabled,
// with its state cleared, in the first block at or after Igloo, whether or not
// genesis enabled it. Without a gas price manager, that block MUST use the
// default gas price config.
func TestFeeManagerDisabledAtIgloo(t *testing.T) {
	// The admin never sends a tx.
	admin := common.Address{'a', 'd', 'm', 'i', 'n'}
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
	zeroFeeConfig := commontype.FeeConfig{
		GasLimit:                 new(big.Int),
		MinBaseFee:               new(big.Int),
		TargetGas:                new(big.Int),
		BaseFeeChangeDenominator: new(big.Int),
		MinBlockGasCost:          new(big.Int),
		MaxBlockGasCost:          new(big.Int),
		BlockGasCostStep:         new(big.Int),
	}

	tests := []struct {
		name                string
		genesisPrecompiles  extras.Precompiles
		wantRoleBefore      allowlist.Role
		wantFeeConfigBefore commontype.FeeConfig
	}{
		{
			name: "enabled_at_genesis",
			genesisPrecompiles: extras.Precompiles{
				feemanager.ConfigKey: &feemanager.Config{
					AllowListConfig: allowlist.AllowListConfig{
						AdminAddresses: []common.Address{admin},
					},
					Upgrade:          precompileconfig.Upgrade{BlockTimestamp: new(uint64(0))},
					InitialFeeConfig: &initialFeeConfig,
				},
			},
			wantRoleBefore:      allowlist.AdminRole,
			wantFeeConfigBefore: initialFeeConfig,
		},
		{
			name:                "not_in_genesis",
			wantRoleBefore:      allowlist.NoRole,
			wantFeeConfigBefore: zeroFeeConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wallet := newWallet(t, 1)
			// The genesis predates Igloo, which activates at the first block.
			iglooTime := upgrade.InitiallyActiveTime.Add(time.Minute)
			timeOpt, _ := withVMTime(iglooTime)
			ctx, sut := newSUT(t,
				withMaxAllocFor(wallet.Addresses()...),
				withIglooTime(iglooTime),
				timeOpt,
				withGenesisPrecompiles(tt.genesisPrecompiles),
			)

			gotRole := sut.fetchAllowListRole(ctx, t, feemanager.ContractAddress, admin, rpc.LatestBlockNumber)
			require.Equal(t, tt.wantRoleBefore, gotRole, "admin role before Igloo")
			require.Equal(t, tt.wantFeeConfigBefore, sut.getStoredFeeConfig(ctx, t), "stored fee config before Igloo")

			// Blocks are only built for pending txs.
			sut.send(ctx, t, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
			blk := sut.RunConsensusLoop(ctx, t)

			require.False(t, sut.isPrecompileEnabledAtLatest(feemanager.ContractAddress), "precompile enabled after Igloo")
			_, gotGasConfig := new(hooks).GasConfigAfter(blk.Header())
			require.Equal(t, gastime.DefaultGasPriceConfig(), gotGasConfig, "gas price config after Igloo")
			gotRole = sut.fetchAllowListRole(ctx, t, feemanager.ContractAddress, admin, rpc.LatestBlockNumber)
			require.Equal(t, allowlist.NoRole, gotRole, "admin role after Igloo")
			require.Equal(t, zeroFeeConfig, sut.getStoredFeeConfig(ctx, t), "stored fee config after Igloo")
		})
	}
}

// getStoredFeeConfig returns the fee config stored by the fee manager at the
// latest block.
func (s *SUT) getStoredFeeConfig(ctx context.Context, tb testing.TB) commontype.FeeConfig {
	tb.Helper()

	stateDB, _, err := s.GethRPCBackends().StateAndHeaderByNumber(ctx, rpc.LatestBlockNumber)
	require.NoErrorf(tb, err, "StateAndHeaderByNumber(%d)", rpc.LatestBlockNumber)
	return feemanager.GetStoredFeeConfig(stateDB)
}
