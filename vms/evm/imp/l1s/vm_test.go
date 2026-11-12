// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"encoding/json"
	"maps"
	"math/big"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blockdelay"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/evm/txgossip/txgossiptest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

var _ saetest.Peer = (*SUT)(nil)

// SUT is the system under test for the L1 [VM]. It bundles the [VM] itself and
// an [ethclient.Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	*vmtest.SUT
}

type (
	sutConfig struct {
		genesis          core.Genesis
		clock            *saetest.Clock
		skipRPCTransport bool
	}
	sutOption = options.Option[sutConfig]
)

// withoutRPCTransport skips the SUT's real HTTP/WebSocket transport. Required
// under testing/synctest, where real sockets never reach a durable block. The
// resulting SUT has no [SUT.EthClient].
func withoutRPCTransport() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.skipRPCTransport = true
	})
}

func withMaxAllocFor(addrs ...common.Address) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		maps.Copy(c.genesis.Alloc, saetest.MaxAllocFor(addrs...))
	})
}

var testStartTime = upgrade.InitiallyActiveTime.Add(time.Duration(acp226.InitialDelayExcess.Delay()) * time.Millisecond)

// withVMTime sets the clock used by the VM and returns it so that tests can
// control it. The option MAY be passed to multiple SUTs so that they share the
// clock.
func withVMTime(startTime time.Time) (sutOption, *saetest.Clock) {
	c := saetest.NewClock(startTime, time.Millisecond)
	opt := options.Func[sutConfig](func(cfg *sutConfig) {
		cfg.clock = c
	})
	return opt, c
}

// newSUT returns a new SUT in [snow.NormalOp]. The returned context is
// cancelled if the VM logs an error.
func newSUT(tb testing.TB, opts ...sutOption) (context.Context, *SUT) {
	tb.Helper()

	cfg := options.ApplyTo(&sutConfig{
		genesis: *testGenesis(),
		clock:   saetest.NewClock(testStartTime, time.Millisecond),
	}, opts...)
	vm := &VM{
		now: cfg.clock.Now,
	}

	snowCtx := snowtest.Context(tb, ids.GenerateTestID())
	snowCtx.ChainDataDir = tb.TempDir()
	snowCtx.NetworkUpgrades = upgradetest.GetConfig(upgradetest.Latest)
	log := loggingtest.New(tb, logging.Debug)
	snowCtx.Log = log

	genesisBytes, err := json.Marshal(cfg.genesis)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.genesis)

	appSender := saetest.NewSender(tb, set.Of(snowCtx.NodeID))

	ctx := log.CancelOnError(tb.Context())
	vmSUT, err := vmtest.New(ctx, tb, vm, snowCtx, vmtest.Config{
		DB:                memdb.New(),
		Genesis:           genesisBytes,
		Sender:            appSender,
		State:             snow.NormalOp,
		SkipRPCTransport:  cfg.skipRPCTransport,
		Clock:             cfg.clock,
		EarliestBuildTime: earliestBuildTime,
	})
	require.NoError(tb, err, "vmtest.New()")
	sut := &SUT{
		VM:  vm,
		SUT: vmSUT,
	}

	appSender.Start(tb, sut)
	return ctx, sut
}

// TestTransfer builds blocks on one SUT and verifies them on another, which
// MUST rebuild the blocks from their headers.
func TestTransfer(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	sender := wallet.Addresses()[0]
	timeOpt, clock := withVMTime(testStartTime)
	var (
		ctx, builder = newSUT(t, withMaxAllocFor(sender), timeOpt)
		_, verifier  = newSUT(t, withMaxAllocFor(sender), timeOpt)
	)

	recipient := common.Address{1}
	const value = 42
	transfer := func() (built, parsed *blocks.Block) {
		t.Helper()

		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &recipient,
			Value:     big.NewInt(value),
			Gas:       ethparams.TxGas,
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, builder.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", builder.EthClient())
		builder.WaitForPendingTxs(ctx, t, tx)

		blk := builder.BuildVerifyAccept(ctx, t)
		// Advancing the clock ensures that the verifier can't rely on it
		// matching the builder's.
		clock.Advance(time.Second)
		parsed = verifier.ParseVerifyAccept(ctx, t, blk)
		for _, b := range []*blocks.Block{blk, parsed} {
			require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		}
		return blk, parsed
	}

	first, _ := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(value), sut.Balance(t, recipient), "balance(recipient) after first transfer")
	}

	clock.AdvanceToSettle(ctx, t, first)
	built, parsed := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(2 * value), sut.Balance(t, recipient), "balance(recipient) after second transfer")
	}
	for _, b := range []*blocks.Block{built, parsed} {
		settled := b.LastSettled()
		require.NotNilf(t, settled, "%T.LastSettled()", b)
		assert.Equal(t, first.Height(), settled.Height(), "%T.LastSettled().Height()", b)
	}
}

// TestMinGasConsumptionFloor asserts that the L1 VM charges the ACP-194 gas
// floor of max(actualGasUsed, ceil(gasLimit/2)).
func TestMinGasConsumptionFloor(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	sender := wallet.Addresses()[0]
	ctx, sut := newSUT(t, withMaxAllocFor(sender))

	const highLimit = 100_000
	tests := []struct {
		name        string
		gasLimit    uint64
		wantGasUsed uint64
	}{
		{
			name:        "low_usage_charged_floor",
			gasLimit:    highLimit,
			wantGasUsed: highLimit / 2,
		},
		{
			name:        "usage_above_floor_charged_actual",
			gasLimit:    ethparams.TxGas,
			wantGasUsed: ethparams.TxGas,
		},
	}

	txs := make([]*types.Transaction, len(tests))
	for i, tt := range tests {
		txs[i] = wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{1},
			Gas:       tt.gasLimit,
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, sut.EthClient().SendTransaction(ctx, txs[i]), "%T.SendTransaction(%s)", sut.EthClient(), tt.name)
	}
	sut.WaitForPendingTxs(ctx, t, txs...)

	blk := sut.BuildVerifyAccept(ctx, t)
	require.NoErrorf(t, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)
	receipts := blk.Receipts()
	require.Lenf(t, receipts, len(tests), "%T.Receipts()", blk)

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, txs[i].Hash(), receipts[i].TxHash, "receipt TxHash")
			assert.Equal(t, tt.wantGasUsed, receipts[i].GasUsed, "receipt GasUsed")
		})
	}
}

// sendTx sends a transfer from wallet's first account and waits for it to be
// pending in sut's mempool.
func sendTx(ctx context.Context, tb testing.TB, sut *SUT, wallet *saetest.Wallet) {
	tb.Helper()

	tx := wallet.SetNonceAndSign(tb, 0, &types.DynamicFeeTx{
		To:        &common.Address{1},
		Gas:       ethparams.TxGas,
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
	})
	require.NoErrorf(tb, sut.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", sut.EthClient())
	txgossiptest.WaitUntilPending(tb, ctx, sut.GethRPCBackends(), tx)
}

func TestVerifyRejectsBlockTimeBelowMinDelay(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...))

	sendTx(ctx, t, sut, wallet)
	parent := sut.BuildVerifyAccept(ctx, t)
	sendTx(ctx, t, sut, wallet)
	child := sut.BuildVerify(ctx, t, parent.ID())

	earlyMS := uint64(earliestBuildTime(parent).UnixMilli()) - 1
	err := sut.VerifyTampered(ctx, t, child, func(hdr *types.Header) {
		hdr.Time = earlyMS / 1000
		customtypes.GetHeaderExtra(hdr).TimeMilliseconds = &earlyMS
	})
	require.ErrorIsf(t, err, blockdelay.ErrBelowMinimum, "%T.VerifyBlock() (block below the ACP-226 minimum delay)", sut.VM)
}

// TestWaitForEventMinDelayPacing asserts that [VM.WaitForEvent] doesn't report
// a pending tx until the preferred block's ACP-226 minimum delay has elapsed.
func TestWaitForEventMinDelayPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Do not build/execute blocks: this test is only synctest-teardown-safe
		// because it runs no blocks (a block can reactivate a goleak-suppressed
		// libevm shutdown race that deadlocks the bubble).
		wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
		timeOpt, _ := withVMTime(upgrade.InitiallyActiveTime)
		ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...), timeOpt, withoutRPCTransport())

		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{1},
			Gas:       ethparams.TxGas,
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, sut.GethRPCBackends().SendTx(ctx, tx), "%T.SendTx()", sut.GethRPCBackends())

		start := time.Now()
		got, err := sut.VM.WaitForEvent(ctx)
		require.NoErrorf(t, err, "%T.WaitForEvent()", sut.VM)
		require.Equalf(t, snowcommon.PendingTxs, got, "%T.WaitForEvent()", sut.VM)
		require.Equalf(t, dynamic.InitialDelayExponent.DelayDuration(), time.Since(start), "%T.WaitForEvent() blocking duration", sut.VM)
	})
}
