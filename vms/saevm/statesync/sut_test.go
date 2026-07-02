// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethcommon "github.com/ava-labs/libevm/common"
)

type (
	// vmSUT is a full VM node.
	vmSUT struct {
		*sae.SinceGenesis[hookstest.Op]
		*sutEnv

		// summaryHandler is the state sync side of this node, sharing the VM's
		// database, network, hooks, and snow context.
		summaryHandler *SummaryHandler

		wallet *saetest.Wallet
	}

	// shSUT is a standalone [SummaryHandler] node with its own network over a
	// fresh database, exactly like a node that is about to state sync, with no
	// VM yet.
	shSUT struct {
		*SummaryHandler
		*network.Network
		*sutEnv
	}

	// sutConfig is shared by all SUT constructors; each reads only the fields
	// relevant to it.
	sutConfig struct {
		enabled        bool
		commitInterval uint64
		avaDB          database.Database
		xdb            saetypes.ExecutionResults
		startTime      time.Time
	}
	sutOption = options.Option[sutConfig]
)

var (
	_ saetest.Peer = (*vmSUT)(nil)
	_ saetest.Peer = (*shSUT)(nil)
)

func withEnabled(e bool) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.enabled = e
	})
}

// withDatabase overrides the default fresh [memdb.New].
func withDatabase(db database.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.avaDB = db
	})
}

func withXDB(h saetypes.ExecutionResults) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.xdb = h
	})
}

// withTime sets the SUT's clock to a specific time at startup.
//
// This is ignored by [newNetworkedSH].
func withTime(t time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.startTime = t
	})
}

// sutEnv holds the pieces common to both SUTs.
type sutEnv struct {
	snowCtx *snow.Context
	hooks   *hookstest.Stub
	keys    *saetest.KeyChain
	genesis *core.Genesis
	db      ethdb.Database
	sender  *saetest.Sender
	clock   *saetest.Clock
}

func (e *sutEnv) NodeID() ids.NodeID      { return e.snowCtx.NodeID }
func (e *sutEnv) Sender() *saetest.Sender { return e.sender }

const (
	genesisTimestamp      = saeparams.TauSeconds
	defaultCommitInterval = 4
)

var (
	// chainID is made a global to keep it constant across multiple SUTs.
	chainID = ids.GenerateTestID()

	// Every genesis allocates a contract account so that all state roots
	// commit to non-trivial code and storage.
	contractAddr    = ethcommon.Address{0xc0, 0xff, 0xee}
	contractStorage = map[ethcommon.Hash]ethcommon.Hash{
		{0x01}: {0xaa},
		{0x02}: {0xbb},
	}
)

func newSUTEnv(t *testing.T, cfg *sutConfig) *sutEnv {
	snowCtx := snowtest.Context(t, chainID)
	snowCtx.Log = loggingtest.New(t, logging.Debug)
	snowCtx.NodeID = ids.GenerateTestNodeID()

	clock := saetest.NewClock(cfg.startTime, time.Nanosecond)
	hooks := hookstest.NewStub(
		1e8,
		hookstest.WithNow(clock.Now),
		hookstest.WithExecutionResultsDBFn(func(string) (saetypes.ExecutionResults, error) {
			return cfg.xdb, nil
		}),
	)

	keys := saetest.NewUNSAFEKeyChain(t, 100) // deterministic
	alloc := saetest.MaxAllocFor(keys.Addresses()...)
	alloc[contractAddr] = types.Account{
		Code:    escrow.ByteCode(),
		Storage: contractStorage,
		Balance: big.NewInt(1),
		Nonce:   1,
	}
	genesis := &core.Genesis{
		Config:     saetest.ChainConfig(),
		Alloc:      alloc,
		Timestamp:  genesisTimestamp,
		BaseFee:    big.NewInt(1),
		Difficulty: big.NewInt(0), // irrelevant but required
	}

	return &sutEnv{
		snowCtx: snowCtx,
		hooks:   hooks,
		keys:    keys,
		genesis: genesis,
		db:      saetypes.NewEthDB(cfg.avaDB),
		sender:  saetest.NewSender(t, nil),
		clock:   clock,
	}
}

func defaultSUTConfig(opts ...sutOption) *sutConfig {
	return options.ApplyTo(&sutConfig{
		enabled:        true,
		commitInterval: defaultCommitInterval,
		avaDB:          memdb.New(),
		xdb:            saetest.NewExecutionResultsDB(),
		startTime:      time.Unix(genesisTimestamp, 0),
	}, opts...)
}

// newSUT constructs a standalone [shSUT] with its own network over an
// otherwise untouched database; no VM is created.
func newSUT(t *testing.T, opts ...sutOption) *shSUT {
	t.Helper()

	cfg := defaultSUTConfig(opts...)
	env := newSUTEnv(t, cfg)

	net, err := network.New(env.snowCtx, env.sender)
	require.NoError(t, err, "network.New()")

	// The [SummaryHandler] requires the genesis block on disk, but the state
	// is deliberately committed to a throwaway trie database so that syncing
	// tests start from an empty state.
	dummyTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	_, err = env.genesis.Commit(env.db, dummyTrieDB)
	require.NoErrorf(t, err, "%T.Commit()", env.genesis)

	sut := &shSUT{
		SummaryHandler: newSummaryHandler(t, cfg, env.snowCtx, env.db, net, env.hooks),
		Network:        net,
		sutEnv:         env,
	}
	env.sender.Start(t, sut)
	return sut
}

func (client *shSUT) syncTo(ctx context.Context, t *testing.T, s *Summary) error {
	mode, err := client.AcceptSummary(ctx, s)
	require.NoErrorf(t, err, "%T.AcceptSummary()", client.SummaryHandler)
	require.Equal(t, block.StateSyncStatic, mode, "AcceptSummary() mode")

	msg, err := client.WaitForEvent(ctx)
	require.NoErrorf(t, err, "%T.WaitForEvent()", client.SummaryHandler)
	require.Equal(t, common.StateSyncDone, msg, "WaitForEvent() message")

	return client.Error()
}

// newSummaryHandler constructs a [SummaryHandler] over the given database and
// network and registers its sync server so peers can sync from it.
func newSummaryHandler(
	t *testing.T,
	cfg *sutConfig,
	snowCtx *snow.Context,
	db ethdb.Database,
	net *network.Network,
	hooks *hookstest.Stub,
) *SummaryHandler {
	t.Helper()

	handler, err := New(
		Config{
			DBConfig: saedb.Config{
				CommitInterval: cfg.commitInterval,
			},
			Enabled: cfg.enabled,
		},
		db,
		snowCtx,
		net,
		hooks,
	)
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoError(t, handler.Shutdown(context.WithoutCancel(t.Context())), "SummaryHandler.Shutdown()")
	})

	tdb := triedb.NewDatabase(db, nil)
	require.NoError(t, handler.RegisterServer(tdb), "RegisterServer()")

	return handler
}

// newVM constructs and initializes a VM, ready for blocks to be accepted with
// [vmSUT.acceptBlocks].
func newVM(t *testing.T, opts ...sutOption) *vmSUT {
	t.Helper()

	cfg := defaultSUTConfig(opts...)
	env := newSUTEnv(t, cfg)
	ctx := t.Context()

	genesisBytes, err := json.Marshal(env.genesis)
	require.NoErrorf(t, err, "json.Marshal(%T)", env.genesis)

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = ""                // no on-disk journal in tests
	vm := sae.NewSinceGenesis(env.hooks, sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			CommitInterval: cfg.commitInterval,
		},
		Now: env.clock.Now,
	})
	require.NoError(t, vm.Initialize(
		ctx,
		env.snowCtx,
		cfg.avaDB,
		genesisBytes,
		nil, // upgrade bytes
		nil, // config bytes
		nil, // fxs
		env.sender,
	), "Initialize()")
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)), "Shutdown()")
	})
	require.NoError(t, vm.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")
	require.NoError(t, vm.SetState(ctx, snow.NormalOp), "SetState(NormalOp)")

	s := &vmSUT{
		SinceGenesis:   vm,
		sutEnv:         env,
		wallet:         saetest.NewWalletWithKeyChain(env.keys, types.LatestSigner(env.genesis.Config)),
		summaryHandler: newSummaryHandler(t, cfg, env.snowCtx, env.db, vm.Network, env.hooks),
	}
	env.sender.Start(t, s)

	return s
}

// lastAcceptedBlock returns the VM's last accepted block, which is the genesis
// block if none have been accepted.
func (s *vmSUT) lastAcceptedBlock(t *testing.T) *blocks.Block {
	t.Helper()
	ctx := t.Context()

	id, err := s.LastAccepted(ctx)
	require.NoError(t, err, "LastAccepted()")
	return s.getBlock(t, id)
}

// blockAtHeight returns the VM's accepted block at the given height.
func (s *vmSUT) blockAtHeight(t *testing.T, height uint64) *blocks.Block {
	t.Helper()
	id, err := s.GetBlockIDAtHeight(t.Context(), height)
	require.NoErrorf(t, err, "GetBlockIDAtHeight(%d)", height)
	return s.getBlock(t, id)
}

// getBlock returns the VM's block with the given ID.
func (s *vmSUT) getBlock(t *testing.T, id ids.ID) *blocks.Block {
	t.Helper()
	b, err := s.GetBlock(t.Context(), id)
	require.NoErrorf(t, err, "GetBlock(%s)", id)
	return b
}

// acceptBlock builds, verifies, and accepts one block on top of the current
// last accepted block, returning it. Every block contains a single transfer.
func (s *vmSUT) acceptBlock(t *testing.T) *blocks.Block {
	t.Helper()
	ctx := t.Context()

	tx := s.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &ethcommon.Address{},
		Value:    big.NewInt(1),
		Gas:      1e6,
		GasPrice: big.NewInt(1),
	})
	backends := s.GethRPCBackends()
	require.NoErrorf(t, backends.SendTx(ctx, tx), "SendTx(%#x)", tx.Hash())
	txgossiptest.WaitUntilPending(t, ctx, backends, tx)

	parent, err := s.LastAccepted(ctx)
	require.NoError(t, err, "LastAccepted()")
	require.NoError(t, s.SetPreference(ctx, parent, nil), "SetPreference()")

	b, err := s.BuildBlock(ctx, nil)
	require.NoErrorf(t, err, "%T.BuildBlock()", s.VM)
	require.Lenf(t, b.Transactions(), 1, "%T.BuildBlock() transactions", s.VM)

	require.NoErrorf(t, s.VerifyBlock(ctx, nil, b), "%T.VerifyBlock()", s.VM)
	require.NoErrorf(t, s.AcceptBlock(ctx, b), "%T.AcceptBlock()", s.VM)

	// Advance the clock so the next accepted block settles this one.
	s.clock.AdvanceToSettle(ctx, t, b)

	return b
}

// acceptBlocks calls [vmSUT.acceptBlock] n times, waiting for the last block to
// finish executing.
func (s *vmSUT) acceptBlocks(t *testing.T, n uint64) {
	t.Helper()
	if n == 0 {
		return
	}

	var last *blocks.Block
	for range n {
		last = s.acceptBlock(t)
	}
	require.NoError(t, last.WaitUntilExecuted(t.Context()), "WaitUntilExecuted()")
}
