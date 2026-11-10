// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vmtest provides a system under test for VMs built atop the SAE VM.
package vmtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/evmclient"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/evm/txgossip/txgossiptest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saerpc "github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// A VM is a VM built atop the SAE VM.
type VM interface {
	adaptor.ChainVM[*blocks.Block]
	GetPreference() *blocks.Block
	LastExecutedState() (libevm.StateReader, error)
	GethRPCBackends() saerpc.GethBackends
}

// A Config configures [New].
type Config struct {
	DB               database.Database
	Genesis          []byte
	Config           []byte
	Sender           *saetest.Sender
	State            snow.State
	HTTPPrefix       string
	SkipRPCTransport bool
	// Clock is the VM's clock. [SUT.WaitForPendingTxs] advances it to
	// EarliestBuildTime of the preferred block, so that tests don't wait in real
	// time for the ACP-226 minimum block delay. Both MUST be set to use
	// [SUT.WaitForPendingTxs].
	Clock *saetest.Clock
	// EarliestBuildTime returns the earliest time at which a child of a block
	// may be built.
	EarliestBuildTime func(*blocks.Block) time.Time
}

// A SUT drives a [VM] as the consensus engine would.
type SUT struct {
	URL       string
	ethclient func() *evmclient.Client

	vm                VM
	nodeID            ids.NodeID
	sender            *saetest.Sender
	clock             *saetest.Clock
	earliestBuildTime func(*blocks.Block) time.Time
}

func (s *SUT) NodeID() ids.NodeID      { return s.nodeID }
func (s *SUT) Sender() *saetest.Sender { return s.sender }

// EthClient dials the VM on first use, which MUST be after the VM is
// bootstrapping.
func (s *SUT) EthClient() *evmclient.Client { return s.ethclient() }

// New initializes vm and transitions it to [Config.State].
func New(ctx context.Context, tb testing.TB, vm VM, snowCtx *snow.Context, cfg Config) (*SUT, error) {
	tb.Helper()

	if err := vm.Initialize(
		ctx,
		snowCtx,
		cfg.DB,
		cfg.Genesis,
		nil, // upgradeBytes
		cfg.Config,
		nil, // fxs
		cfg.Sender,
	); err != nil {
		return nil, fmt.Errorf("%T.Initialize(): %w", vm, err)
	}
	tb.Cleanup(func() {
		// The context is cancelled before cleanup is called, so we strip the
		// cancellation.
		ctx := context.WithoutCancel(tb.Context())
		require.NoErrorf(tb, vm.Shutdown(ctx), "%T.Shutdown()", vm)
	})

	// Avalanchego marks the local node as connected so that p2p protocols don't
	// need to treat our node as a special case.
	require.NoErrorf(tb, vm.Connected(ctx, snowCtx.NodeID, version.Current), "%T.Connected(%s)", vm, snowCtx.NodeID)

	sut := &SUT{
		ethclient:         func() *evmclient.Client { return nil },
		vm:                vm,
		nodeID:            snowCtx.NodeID,
		sender:            cfg.Sender,
		clock:             cfg.Clock,
		earliestBuildTime: cfg.EarliestBuildTime,
	}

	if !cfg.SkipRPCTransport {
		handlers, err := vm.CreateHandlers(ctx)
		require.NoErrorf(tb, err, "%T.CreateHandlers()", vm)

		mux := http.NewServeMux()
		for path, h := range handlers {
			mux.Handle(cfg.HTTPPrefix+path, h)
		}
		server := httptest.NewServer(mux)
		tb.Cleanup(server.Close)
		sut.URL = server.URL

		sut.ethclient = sync.OnceValue(func() *evmclient.Client {
			wsHTTPPath := cfg.HTTPPrefix + "/ws"
			wsURI := "ws://" + server.Listener.Addr().String() + wsHTTPPath
			client, err := evmclient.Dial(tb.Context(), wsURI)
			require.NoErrorf(tb, err, "evmclient.Dial(%s)", wsURI)
			tb.Cleanup(client.Close)
			return client
		})
	}

	if cfg.State == snow.NormalOp {
		// The engine sets the preference to the last accepted block when entering
		// normal operation. The bootstrapper will first set it to bootstrapping.
		if err := vm.SetState(ctx, snow.Bootstrapping); err != nil {
			return nil, fmt.Errorf("%T.SetState(%s): %w", vm, snow.Bootstrapping, err)
		}
		lastAccepted, err := vm.LastAccepted(ctx)
		require.NoErrorf(tb, err, "%T.LastAccepted()", vm)
		require.NoErrorf(tb, vm.SetPreference(ctx, lastAccepted, nil), "%T.SetPreference()", vm)
	}
	if err := vm.SetState(ctx, cfg.State); err != nil {
		return nil, fmt.Errorf("%T.SetState(%s): %w", vm, cfg.State, err)
	}
	return sut, nil
}

type (
	blockConfig struct {
		context *block.Context
	}
	// A BlockOption configures [SUT.BuildVerify].
	BlockOption = options.Option[blockConfig]
)

// WithBlockContext sets the [block.Context] used to set the preference and to
// build and verify the block. If unset, a nil context is used.
func WithBlockContext(ctx *block.Context) BlockOption {
	return options.Func[blockConfig](func(c *blockConfig) {
		c.context = ctx
	})
}

// BuildVerify builds and verifies a block on top of preferenceID.
func (s *SUT) BuildVerify(ctx context.Context, tb testing.TB, preferenceID ids.ID, opts ...BlockOption) *blocks.Block {
	tb.Helper()

	blockContext := options.As(opts...).context
	require.NoErrorf(tb, s.vm.SetPreference(ctx, preferenceID, blockContext), "%T.SetPreference()", s.vm)

	s.WaitForPendingTxs(ctx, tb)
	blk, err := s.vm.BuildBlock(ctx, blockContext)
	require.NoErrorf(tb, err, "%T.BuildBlock()", s.vm)
	require.NoErrorf(tb, s.vm.VerifyBlock(ctx, blockContext, blk), "%T.VerifyBlock()", s.vm)
	return blk
}

// ParseVerifyAccept drives a block produced by another node through
// this SUT's consensus surface, as the engine would during bootstrapping or
// normal operation.
func (s *SUT) ParseVerifyAccept(ctx context.Context, tb testing.TB, blk *blocks.Block) *blocks.Block {
	tb.Helper()

	parsed, err := s.vm.ParseBlock(ctx, blk.Bytes())
	require.NoErrorf(tb, err, "%T.ParseBlock(height %d)", s.vm, blk.Height())
	require.NoErrorf(tb, s.vm.VerifyBlock(ctx, nil, parsed), "%T.VerifyBlock(height %d)", s.vm, blk.Height())
	require.NoErrorf(tb, s.vm.AcceptBlock(ctx, parsed), "%T.AcceptBlock(height %d)", s.vm, blk.Height())
	return parsed
}

// BuildVerifyAccept builds, verifies, and accepts a block on top of the
// last-accepted block.
func (s *SUT) BuildVerifyAccept(ctx context.Context, tb testing.TB, opts ...BlockOption) *blocks.Block {
	tb.Helper()

	lastAccepted := s.LastAcceptedID(ctx, tb)
	blk := s.BuildVerify(ctx, tb, lastAccepted, opts...)
	require.NoErrorf(tb, s.vm.AcceptBlock(ctx, blk), "%T.AcceptBlock()", s.vm)
	return blk
}

// AcceptAndExecute accepts blk and blocks until it has been executed.
func (s *SUT) AcceptAndExecute(ctx context.Context, tb testing.TB, blk *blocks.Block) {
	tb.Helper()

	require.NoErrorf(tb, s.vm.AcceptBlock(ctx, blk), "%T.AcceptBlock()", s.vm)
	require.NoErrorf(tb, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)
}

// RunConsensusLoop builds a block on top of the last-accepted block, drives it
// through verify+accept, and waits until it has been executed.
func (s *SUT) RunConsensusLoop(ctx context.Context, tb testing.TB, opts ...BlockOption) *blocks.Block {
	tb.Helper()

	blk := s.BuildVerifyAccept(ctx, tb, opts...)
	require.NoErrorf(tb, blk.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", blk)
	return blk
}

// LastAcceptedID returns the ID of the last-accepted block.
func (s *SUT) LastAcceptedID(ctx context.Context, tb testing.TB) ids.ID {
	tb.Helper()

	id, err := s.vm.LastAccepted(ctx)
	require.NoErrorf(tb, err, "%T.LastAccepted()", s.vm)
	return id
}

// LastAcceptedHeight returns the height of the last-accepted block.
func (s *SUT) LastAcceptedHeight(ctx context.Context, tb testing.TB) uint64 {
	tb.Helper()

	blk, err := s.vm.GetBlock(ctx, s.LastAcceptedID(ctx, tb))
	require.NoErrorf(tb, err, "%T.GetBlock(last accepted)", s.vm)
	return blk.Height()
}

// BlockAtHeight returns the accepted block at the given height.
func (s *SUT) BlockAtHeight(ctx context.Context, tb testing.TB, height uint64) *blocks.Block {
	tb.Helper()

	id, err := s.vm.GetBlockIDAtHeight(ctx, height)
	require.NoErrorf(tb, err, "%T.GetBlockIDAtHeight(%d)", s.vm, height)
	blk, err := s.vm.GetBlock(ctx, id)
	require.NoErrorf(tb, err, "%T.GetBlock(%s)", s.vm, id)
	return blk
}

// Balance returns the balance of addr at the last-executed state.
func (s *SUT) Balance(tb testing.TB, addr common.Address) uint256.Int {
	tb.Helper()

	state, err := s.vm.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.vm)
	return *state.GetBalance(addr)
}

// WaitForPendingTxs waits for [VM.WaitForEvent] to report pending txs. Any txs
// provided are first waited on to be in the tx pool, so that the next built
// block includes all of them rather than racing promotion.
func (s *SUT) WaitForPendingTxs(ctx context.Context, tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()

	txgossiptest.WaitUntilPending(tb, ctx, s.vm.GethRPCBackends(), txs...)
	if t := s.earliestBuildTime(s.vm.GetPreference()); s.clock.Now().Before(t) {
		s.clock.Set(t)
	}
	e, err := s.vm.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.vm)
	assert.Equalf(tb, snowcommon.PendingTxs, e, "%T.WaitForEvent() event", s.vm)
}

// AssertAccount asserts addr's nonce and balance at the last-executed state.
func (s *SUT) AssertAccount(tb testing.TB, addr common.Address, wantNonce uint64, wantBalance uint256.Int) {
	tb.Helper()

	state, err := s.vm.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.vm)

	gotNonce := state.GetNonce(addr)
	assert.Equalf(tb, wantNonce, gotNonce, "nonce of %s", addr)

	gotBalance := *state.GetBalance(addr)
	assert.Equalf(tb, wantBalance, gotBalance, "balance of %s", addr)
}

// VerifyTampered applies tamper to a copy of valid's header, re-seals the block,
// and returns the error from verifying it.
func (s *SUT) VerifyTampered(ctx context.Context, tb testing.TB, valid *blocks.Block, tamper func(*types.Header)) error {
	tb.Helper()

	hdr := valid.Header()
	tamper(hdr)

	buf, err := rlp.EncodeToBytes(valid.EthBlock().WithSeal(hdr))
	require.NoErrorf(tb, err, "rlp.EncodeToBytes(tampered block)")
	parsed, err := s.vm.ParseBlock(ctx, buf)
	require.NoErrorf(tb, err, "%T.ParseBlock(tampered block)", s.vm)
	return s.vm.VerifyBlock(ctx, nil, parsed)
}
