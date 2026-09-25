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
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/vmtest"

	ethparams "github.com/ava-labs/libevm/params"
)

var _ saetest.Peer = (*SUT)(nil)

// SUT is the system under test for the L1 [VM]. It bundles the [VM] itself and
// an [ethclient.Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	*vmtest.SUT

	ctx *snow.Context
}

type (
	sutConfig struct {
		genesis core.Genesis
		clock   *saetest.Clock
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

	appSender := saetest.NewSender(tb, set.Of(snowCtx.NodeID))

	ctx := log.CancelOnError(tb.Context())
	vmSUT, err := vmtest.New(ctx, tb, vm, snowCtx, vmtest.Config{
		DB:      memdb.New(),
		Genesis: genesisBytes,
		Sender:  appSender,
		State:   snow.NormalOp,
	})
	require.NoError(tb, err, "vmtest.New()")
	sut := &SUT{
		VM:  vm,
		SUT: vmSUT,
		ctx: snowCtx,
	}

	appSender.Start(tb, sut)
	return ctx, sut
}

// hooks returns a new [hooks] instance that behaves equivalently to those
// provided to the sae VM.
func (s *SUT) hooks(tb testing.TB) *hooks {
	tb.Helper()
	return newHooks(s.ctx, s.now)
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
	transfer := func() *blocks.Block {
		t.Helper()

		tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &recipient,
			Value:     big.NewInt(value),
			Gas:       ethparams.TxGas,
			GasFeeCap: big.NewInt(1_000 * ethparams.GWei),
		})
		require.NoErrorf(t, builder.EthClient().SendTransaction(ctx, tx), "%T.SendTransaction()", builder.EthClient())
		builder.WaitForPendingEthTxs(ctx, t, tx)

		blk := builder.BuildVerifyAccept(ctx, t)
		// Advancing the clock ensures that the verifier can't rely on it
		// matching the builder's.
		clock.Advance(time.Second)
		parsed := verifier.ParseVerifyAccept(ctx, t, blk)
		for _, b := range []*blocks.Block{blk, parsed} {
			require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		}
		return blk
	}

	first := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(value), sut.Balance(t, recipient), "balance(recipient) after first transfer")
	}

	clock.AdvanceToSettle(ctx, t, first)
	second := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(2 * value), sut.Balance(t, recipient), "balance(recipient) after second transfer")
		assert.Equal(t, first.Height(), sut.hooks(t).SettledBy(second.Header()).Height, "SettledBy(second).Height")
	}
}
