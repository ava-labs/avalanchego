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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	ethrpc "github.com/ava-labs/libevm/rpc"
)

// A VM is a VM built atop the SAE VM.
type VM interface {
	adaptor.ChainVM[*blocks.Block]
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
	// BeforeWaitForEvent, if non-nil, is called before each [VM.WaitForEvent].
	BeforeWaitForEvent func()
}

// A SUT drives a [VM] as the consensus engine would.
type SUT struct {
	URL       string
	ethclient func() *ethclient.Client

	vm                 VM
	nodeID             ids.NodeID
	sender             *saetest.Sender
	beforeWaitForEvent func()
}

func (s *SUT) NodeID() ids.NodeID      { return s.nodeID }
func (s *SUT) Sender() *saetest.Sender { return s.sender }

// EthClient dials the VM on first use, which MUST be after the VM is
// bootstrapping.
func (s *SUT) EthClient() *ethclient.Client { return s.ethclient() }

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

	// This is called immediately after initialization by avalanchego, so we
	// should test this behavior specifically.
	handlers, err := vm.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", vm)

	// Avalanchego marks the local node as connected so that p2p protocols don't
	// need to treat our node as a special case.
	require.NoErrorf(tb, vm.Connected(ctx, snowCtx.NodeID, version.Current), "%T.Connected(%s)", vm, snowCtx.NodeID)

	sut := &SUT{
		ethclient:          func() *ethclient.Client { return nil },
		vm:                 vm,
		nodeID:             snowCtx.NodeID,
		sender:             cfg.Sender,
		beforeWaitForEvent: cfg.BeforeWaitForEvent,
	}

	if !cfg.SkipRPCTransport {
		mux := http.NewServeMux()
		for path, h := range handlers {
			mux.Handle(cfg.HTTPPrefix+path, h)
		}
		server := httptest.NewServer(mux)
		tb.Cleanup(server.Close)
		sut.URL = server.URL

		sut.ethclient = sync.OnceValue(func() *ethclient.Client {
			wsHTTPPath := cfg.HTTPPrefix + "/ws"
			wsURI := "ws://" + server.Listener.Addr().String() + wsHTTPPath
			ethRPCClient, err := ethrpc.Dial(wsURI)
			require.NoErrorf(tb, err, "rpc.Dial(%s)", wsURI)
			tb.Cleanup(ethRPCClient.Close)

			return ethclient.NewClient(ethRPCClient)
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

// Balance returns the balance of addr at the last-executed state.
func (s *SUT) Balance(tb testing.TB, addr common.Address) uint256.Int {
	tb.Helper()

	state, err := s.vm.LastExecutedState()
	require.NoErrorf(tb, err, "%T.LastExecutedState()", s.vm)
	return *state.GetBalance(addr)
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

// LastAcceptedID returns the ID of the last-accepted block.
func (s *SUT) LastAcceptedID(ctx context.Context, tb testing.TB) ids.ID {
	tb.Helper()

	id, err := s.vm.LastAccepted(ctx)
	require.NoErrorf(tb, err, "%T.LastAccepted()", s.vm)
	return id
}

// WaitForPendingTxs waits for [VM.WaitForEvent] to report pending txs.
func (s *SUT) WaitForPendingTxs(ctx context.Context, tb testing.TB) {
	tb.Helper()

	if s.beforeWaitForEvent != nil {
		s.beforeWaitForEvent()
	}
	e, err := s.vm.WaitForEvent(ctx)
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.vm)
	assert.Equalf(tb, snowcommon.PendingTxs, e, "%T.WaitForEvent() event", s.vm)
}

// WaitForPendingEthTxs blocks until every tx is pending in the source the block
// builder draws from, so the built block includes them all rather than racing
// promotion. The geth RPC backend's [GetPoolTransactions] resolves the same
// [txpool.Pool.Pending] set used by [txgossip.Set.TransactionsByPriority]
// during block building.
func (s *SUT) WaitForPendingEthTxs(ctx context.Context, tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	txgossiptest.WaitUntilPending(tb, ctx, s.vm.GethRPCBackends(), txs...)
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
