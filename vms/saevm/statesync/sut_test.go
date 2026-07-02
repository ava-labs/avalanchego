// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
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
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
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
)

// chainID is made a global to keep it constant across multiple SUTs, ensuring
// they agree on the genesis block.
var chainID = ids.GenerateTestID()

// Every genesis allocates a contract account so that all state roots
// commit to non-trivial code and storage.
var (
	contractAddr    = common.Address{0xc0, 0xff, 0xee}
	contractStorage = map[common.Hash]common.Hash{
		{0x01}: {0xaa},
		{0x02}: {0xbb},
	}
)

const defaultCommitInterval = 4

type (
	// vmSUT is a full VM node. It implements [saetest.Peer] via the embedded
	// network so that SUTs can be wired together with [saetest.ConnectTo].
	// Every node also runs its state sync side: [vmSUT.summaryHandler] is built
	// over the same database and network, with its sync server registered so
	// peers can sync from it.
	vmSUT struct {
		*sae.SinceGenesis[hookstest.Op]

		// summaryHandler is the state sync side of this node, sharing the VM's
		// database, network, hooks, and snow context.
		summaryHandler *SummaryHandler

		hooks   *hookstest.Stub
		wallet  *saetest.Wallet
		clock   *saetest.Clock
		genesis *core.Genesis
		avaDB   database.Database
		db      ethdb.Database
		sender  *saetest.Sender
		snowCtx *snow.Context
		saeCfg  sae.Config
	}

	// networkedSH is a standalone [SummaryHandler] node with its own network over a
	// fresh database, exactly like a node that is about to state sync, with no
	// VM yet. It implements [saetest.Peer] via the embedded network. A running
	// node's state sync side is instead [vmSUT.summaryHandler].
	networkedSH struct {
		*SummaryHandler
		*network.Network

		hooks   *hookstest.Stub
		genesis *core.Genesis
		db      ethdb.Database
		sender  *saetest.Sender
		snowCtx *snow.Context
	}

	// sutConfig is shared by all SUT constructors; each reads only the fields
	// relevant to it.
	sutConfig struct {
		enabled        *bool
		commitInterval uint64
		avaDB          database.Database
		xdb            saetypes.ExecutionResults
		startTime      time.Time
	}
	sutOption = options.Option[sutConfig]
)

var (
	_ saetest.Peer = (*vmSUT)(nil)
	_ saetest.Peer = (*networkedSH)(nil)
)

func (s *vmSUT) NodeID() ids.NodeID            { return s.snowCtx.NodeID }
func (s *vmSUT) Sender() *saetest.Sender       { return s.sender }
func (s *networkedSH) NodeID() ids.NodeID      { return s.snowCtx.NodeID }
func (s *networkedSH) Sender() *saetest.Sender { return s.sender }

func withEnabled(e *bool) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.enabled = e
	})
}

// withDatabase overrides the default fresh [memdb.New], e.g. to start a VM
// over an already-synced database.
func withDatabase(db database.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.avaDB = db
	})
}

// withXDB allows a test to provide its own [saetypes.ExecutionResults].
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

// sutEnv holds the pieces common to both SUT flavours.
type sutEnv struct {
	snowCtx *snow.Context
	hooks   *hookstest.Stub
	keys    *saetest.KeyChain
	genesis *core.Genesis
	avaDB   database.Database
	db      ethdb.Database
	sender  *saetest.Sender
	saeCfg  sae.Config
	clock   *saetest.Clock
}

const genesisTimestamp = saeparams.TauSeconds

func newSUTEnv(t *testing.T, cfg *sutConfig) *sutEnv {
	log := loggingtest.New(t, logging.Info)

	snowCtx := snowtest.Context(t, chainID)
	snowCtx.Log = log
	snowCtx.NodeID = ids.GenerateTestNodeID()

	clock := saetest.NewClock(cfg.startTime, time.Nanosecond)
	hooks := hookstest.NewStub(
		1e8,
		hookstest.WithNow(clock.Now),
		hookstest.WithExecutionResultsDBFn(func(string) (saetypes.ExecutionResults, error) {
			return cfg.xdb, nil
		}),
	)

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = ""                // no on-disk journal in tests
	saeCfg := sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			TrieCommitInterval: cfg.commitInterval,
		},
		Now: clock.Now,
	}

	keys := saetest.NewUNSAFEKeyChain(t, 100) // deterministic, so all SUTs agree on the alloc
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
		avaDB:   cfg.avaDB,
		db:      saetypes.NewEthDB(cfg.avaDB),
		sender:  saetest.NewSender(t, nil),
		saeCfg:  saeCfg,
		clock:   clock,
	}
}

func defaultSUTConfig(opts ...sutOption) *sutConfig {
	return options.ApplyTo(&sutConfig{
		enabled:        utils.PointerTo(true),
		commitInterval: defaultCommitInterval,
		avaDB:          memdb.New(),
		xdb:            saetest.NewExecutionResultsDB(),
		startTime:      time.Unix(genesisTimestamp, 0),
	}, opts...)
}

// newNetworkedSH constructs a standalone [networkedSH] with its own network over an
// otherwise untouched database; no VM is created.
func newNetworkedSH(t *testing.T, opts ...sutOption) *networkedSH {
	t.Helper()

	cfg := defaultSUTConfig(opts...)
	env := newSUTEnv(t, cfg)

	net, err := network.New(env.snowCtx, env.sender)
	require.NoError(t, err, "network.New()")

	sh := &networkedSH{
		Network: net,
		hooks:   env.hooks,
		genesis: env.genesis,
		db:      env.db,
		sender:  env.sender,
		snowCtx: env.snowCtx,
	}
	sh.SummaryHandler = newSummaryHandler(t, cfg, sh.snowCtx, sh.db, sh.genesis, sh.Network, sh.hooks)
	env.sender.Start(t, sh)
	return sh
}

// newSummaryHandler constructs a [SummaryHandler] over the given database and
// network and registers its sync server so peers can sync from it.
func newSummaryHandler(
	t *testing.T,
	cfg *sutConfig,
	snowCtx *snow.Context,
	db ethdb.Database,
	genesis *core.Genesis,
	net *network.Network,
	hooks *hookstest.Stub,
) *SummaryHandler {
	t.Helper()

	handler, err := New(
		Config{
			CommitInterval: cfg.commitInterval,
			Enabled:        cfg.enabled,
		},
		snowCtx,
		db,
		genesis.ToBlock(),
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

// newVM constructs and initializes a VM, then builds and accepts the
// number of blocks requested, each block settling the previous.
func newVM(t *testing.T, opts ...sutOption) *vmSUT {
	t.Helper()

	cfg := defaultSUTConfig(opts...)
	env := newSUTEnv(t, cfg)
	ctx := t.Context()

	vm := sae.NewSinceGenesis(env.hooks, env.saeCfg)
	require.NoError(t, vm.Initialize(
		ctx,
		env.snowCtx,
		env.avaDB,
		marshalJSON(t, env.genesis),
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
		SinceGenesis: vm,
		hooks:        env.hooks,
		wallet:       saetest.NewWalletWithKeyChain(env.keys, types.LatestSigner(env.genesis.Config)),
		clock:        env.clock,
		genesis:      env.genesis,
		avaDB:        env.avaDB,
		db:           env.db,
		sender:       env.sender,
		snowCtx:      env.snowCtx,
		saeCfg:       env.saeCfg,
	}
	env.sender.Start(t, s)

	s.summaryHandler = newSummaryHandler(t, cfg, s.snowCtx, s.db, s.genesis, s.Network, s.hooks)

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
		To:       &common.Address{},
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

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	buf, err := json.Marshal(v)
	require.NoErrorf(t, err, "json.Marshal(%T)", v)
	return buf
}
