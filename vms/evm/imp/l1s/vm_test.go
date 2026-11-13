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
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/stateupgrade"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"

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
		genesis  core.Genesis
		upgrades extras.UpgradeConfig
		clock    *saetest.Clock
	}
	sutOption = options.Option[sutConfig]
)

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
	upgradeBytes, err := json.Marshal(cfg.upgrades)
	require.NoErrorf(tb, err, "json.Marshal(%T)", cfg.upgrades)

	appSender := saetest.NewSender(tb, set.Of(snowCtx.NodeID))

	ctx := log.CancelOnError(tb.Context())
	vmSUT, err := vmtest.New(ctx, tb, vm, snowCtx, vmtest.Config{
		DB:       memdb.New(),
		Genesis:  genesisBytes,
		Upgrades: upgradeBytes,
		Sender:   appSender,
		State:    snow.NormalOp,
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

	recipient := common.Address{2}
	const value = 42
	transfer := func() (built, parsed *blocks.Block) {
		t.Helper()

		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &recipient,
			Value:     big.NewInt(value),
			Gas:       ethparams.TxGas,
			GasTipCap: big.NewInt(1),
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
			GasTipCap: big.NewInt(1),
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

// TestStateUpgradeAppliedasserts that a state upgrade activating in a block is
// applied before that block's transactions are executed.
func TestStateUpgradeApplied(t *testing.T) {
	var (
		upgradeAddr = common.Address{'t', 'a', 'r', 'g', 'e', 't'}
		marker      = []byte("upgraded")
		code        = saetest.LogTopOfStackAfter(saetest.Push(t, marker))
	)

	// upgradeTime MUST be after genesis.
	upgradeTime := time.Unix(int64(testGenesisTime)+10, 0) //#nosec G115 -- Won't overflow
	upgradeUnix := uint64(upgradeTime.Unix())              //#nosec G115 -- Known non-negative

	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	timeOpt, _ := withVMTime(upgradeTime)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		timeOpt,
		options.Func[sutConfig](func(c *sutConfig) {
			c.upgrades.StateUpgrades = []stateupgrade.Block{{
				Timestamp: &upgradeUnix,
				Accounts: map[common.Address]stateupgrade.Account{
					upgradeAddr: {Code: code},
				},
			}}
		}),
	)

	tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &upgradeAddr,
		Gas:       100_000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
	})
	require.NoErrorf(t, sut.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", sut.EthClient())
	sut.WaitForPendingTxs(ctx, t, tx)

	blk := sut.BuildVerifyAccept(ctx, t)
	require.Equal(t, upgradeUnix, blk.Header().Time, "block time MUST activate the state upgrade")
	require.NoErrorf(t, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)

	receipts := blk.Receipts()
	require.Lenf(t, receipts, 1, "%T.Receipts()", blk)
	r := receipts[0]
	require.Equal(t, types.ReceiptStatusSuccessful, r.Status, "receipt status")
	require.Len(t, r.Logs, 1, "receipt logs")
	log := r.Logs[0]
	assert.Equal(t, upgradeAddr, log.Address, "log address")
	assert.Equal(t, []common.Hash{common.BytesToHash(marker)}, log.Topics, "log topics")
}

// TestPrecompileActivation asserts that a precompile upgrade activates at the
// time configured in the upgrade config.
func TestPrecompileActivation(t *testing.T) {
	var (
		recipient = common.Address{'r', 'e', 'c', 'i', 'p', 'i', 'e', 'n', 't'}
		amount    = uint256.NewInt(42)
	)

	// upgradeTime MUST be after genesis.
	upgradeTime := time.Unix(int64(testGenesisTime)+10, 0) //#nosec G115 -- Won't overflow
	upgradeUnix := uint64(upgradeTime.Unix())              //#nosec G115 -- Known non-negative

	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSignerForChainID(big.NewInt(testChainID)))
	timeOpt, _ := withVMTime(upgradeTime)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		timeOpt,
		options.Func[sutConfig](func(c *sutConfig) {
			c.upgrades.PrecompileUpgrades = []extras.PrecompileUpgrade{{
				Config: nativeminter.NewConfig(&upgradeUnix, nil, nil, nil, map[common.Address]*math.HexOrDecimal256{
					recipient: (*math.HexOrDecimal256)(amount.ToBig()),
				}),
			}}
		}),
	)

	// tx is unrelated, but block can't be empty.
	tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &common.Address{1},
		Gas:       ethparams.TxGas,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
	})
	require.NoErrorf(t, sut.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", sut.EthClient())
	sut.WaitForPendingTxs(ctx, t, tx)

	activating := sut.BuildVerifyAccept(ctx, t)
	require.NoErrorf(t, activating.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", activating)

	require.Equal(t, upgradeUnix, activating.Header().Time, "block time MUST activate the precompile")
	assert.Equal(t, *amount, sut.Balance(t, recipient), "balance(recipient) after activating block")
}
