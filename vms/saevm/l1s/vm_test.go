// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
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
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
	ethrpc "github.com/ava-labs/libevm/rpc"
)

var _ saetest.Peer = (*SUT)(nil)

// SUT is the system under test for the L1 [VM]. It bundles the [VM] itself and
// an [ethclient.Client] connected to an in-process [httptest.Server].
type SUT struct {
	*VM
	ethclient  *ethclient.Client
	clientOnce func()

	ctx    *snow.Context
	sender *saetest.Sender
}

func (s *SUT) NodeID() ids.NodeID      { return s.ctx.NodeID }
func (s *SUT) Sender() *saetest.Sender { return s.sender }

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
	require.NoErrorf(tb, vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		appSender,
	), "%T.Initialize()", vm)
	tb.Cleanup(func() {
		// The context is cancelled before cleanup is called, so we strip the
		// cancellation.
		ctx := context.WithoutCancel(tb.Context())
		require.NoErrorf(tb, vm.Shutdown(ctx), "%T.Shutdown()", vm)
	})

	// This is called immediately after initialization by avalanchego, so we
	// should test this behavior specifically.
	handlers, err := vm.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", vm)

	// Avalanchego marks the local node as connected so that p2p protocols don't
	// need to treat our node as a special case.
	require.NoErrorf(tb, vm.Connected(ctx, snowCtx.NodeID, version.Current), "%T.Connected(%s)", vm, snowCtx.NodeID)

	sut := &SUT{
		VM:     vm,
		ctx:    snowCtx,
		sender: appSender,
	}

	// Called from [SUT.SetState].
	sut.clientOnce = sync.OnceFunc(func() {
		mux := http.NewServeMux()
		for path, h := range handlers {
			mux.Handle(path, h)
		}
		server := httptest.NewServer(mux)
		tb.Cleanup(server.Close)

		wsURI := "ws://" + server.Listener.Addr().String() + "/ws"
		ethRPCClient, err := ethrpc.Dial(wsURI)
		require.NoErrorf(tb, err, "rpc.Dial(%s)", wsURI)
		tb.Cleanup(ethRPCClient.Close)

		sut.ethclient = ethclient.NewClient(ethRPCClient)
	})

	// The engine sets the preference to the last accepted block when entering
	// normal operation. The bootstrapper will first set it to bootstrapping.
	require.NoErrorf(tb, sut.SetState(ctx, snow.Bootstrapping), "%T.SetState(%s)", vm, snow.Bootstrapping)
	require.NoErrorf(tb, sut.SetPreference(ctx, sut.lastAccepted(ctx, tb), nil), "%T.SetPreference()", vm)
	require.NoErrorf(tb, sut.SetState(ctx, snow.NormalOp), "%T.SetState(%s)", vm, snow.NormalOp)

	appSender.Start(tb, sut)
	return ctx, sut
}

func (s *SUT) SetState(ctx context.Context, state snow.State) error {
	if err := s.VM.SetState(ctx, state); err != nil {
		return err
	}
	if state >= snow.Bootstrapping {
		s.clientOnce()
	}
	return nil
}

// hooks returns a new [hooks] instance that behaves equivalently to those
// provided to the sae VM.
func (s *SUT) hooks(tb testing.TB) *hooks {
	tb.Helper()
	return newHooks(s.ctx, s.now)
}

// balance returns the balance of addr at the last-executed state.
func (s *SUT) balance(tb testing.TB, addr common.Address) uint256.Int {
	tb.Helper()

	state, err := s.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.VM)
	return *state.GetBalance(addr)
}

// buildVerifyAccept builds, verifies, and accepts a block on top of the
// last-accepted block.
func (s *SUT) buildVerifyAccept(ctx context.Context, tb testing.TB) *blocks.Block {
	tb.Helper()

	blk := s.buildVerify(ctx, tb, s.lastAccepted(ctx, tb))
	require.NoErrorf(tb, s.AcceptBlock(ctx, blk), "%T.AcceptBlock()", s.VM)
	return blk
}

// lastAccepted returns the ID of the last-accepted block.
func (s *SUT) lastAccepted(ctx context.Context, tb testing.TB) ids.ID {
	tb.Helper()

	id, err := s.LastAccepted(ctx)
	require.NoErrorf(tb, err, "%T.LastAccepted()", s.VM)
	return id
}

func (s *SUT) waitForPendingTxs(ctx context.Context, tb testing.TB) {
	tb.Helper()

	e, err := s.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.VM)
	assert.Equalf(tb, snowcommon.PendingTxs, e, "%T.WaitForEvent() event", s.VM)
}

// waitForPendingEthTxs blocks until every tx is pending in the source the block
// builder draws from, so the built block includes them all rather than racing
// promotion.
func (s *SUT) waitForPendingEthTxs(ctx context.Context, tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	txgossiptest.WaitUntilPending(tb, ctx, s.GethRPCBackends(), txs...)
}

// buildVerify builds and verifies a block on top of preferenceID.
func (s *SUT) buildVerify(ctx context.Context, tb testing.TB, preferenceID ids.ID) *blocks.Block {
	tb.Helper()

	require.NoErrorf(tb, s.SetPreference(ctx, preferenceID, nil), "%T.SetPreference()", s.VM)

	s.waitForPendingTxs(ctx, tb)
	blk, err := s.BuildBlock(ctx, nil)
	require.NoErrorf(tb, err, "%T.BuildBlock()", s.VM)
	require.NoErrorf(tb, s.VerifyBlock(ctx, nil, blk), "%T.VerifyBlock()", s.VM)
	return blk
}

// parseVerifyAccept drives a block produced by another node through this SUT's
// consensus surface, as the engine would during bootstrapping or normal
// operation.
func (s *SUT) parseVerifyAccept(ctx context.Context, tb testing.TB, blk *blocks.Block) *blocks.Block {
	tb.Helper()

	parsed, err := s.ParseBlock(ctx, blk.Bytes())
	require.NoErrorf(tb, err, "%T.ParseBlock(height %d)", s.VM, blk.Height())
	require.NoErrorf(tb, s.VerifyBlock(ctx, nil, parsed), "%T.VerifyBlock(height %d)", s.VM, blk.Height())
	require.NoErrorf(tb, s.AcceptBlock(ctx, parsed), "%T.AcceptBlock(height %d)", s.VM, blk.Height())
	return parsed
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
		require.NoErrorf(t, builder.ethclient.SendTransaction(ctx, tx), "%T.SendTransaction()", builder.ethclient)
		builder.waitForPendingEthTxs(ctx, t, tx)

		blk := builder.buildVerifyAccept(ctx, t)
		// Advancing the clock ensures that the verifier can't rely on it
		// matching the builder's.
		clock.Advance(time.Second)
		parsed := verifier.parseVerifyAccept(ctx, t, blk)
		for _, b := range []*blocks.Block{blk, parsed} {
			require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		}
		return blk
	}

	first := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(value), sut.balance(t, recipient), "balance(recipient) after first transfer")
	}

	clock.AdvanceToSettle(ctx, t, first)
	second := transfer()
	for _, sut := range []*SUT{builder, verifier} {
		assert.Equal(t, *uint256.NewInt(2 * value), sut.balance(t, recipient), "balance(recipient) after second transfer")
		assert.Equal(t, first.Height(), sut.hooks(t).SettledBy(second.Header()).Height, "SettledBy(second).Height")
	}
}
