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

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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
	sut struct {
		*SummaryHandler
		*network.Network

		cfg     *sutConfig
		snowCtx *snow.Context
		hooks   *hookstest.Stub
		keys    *saetest.KeyChain
		genesis *core.Genesis
		db      ethdb.Database
		sender  *saetest.Sender
		clock   *saetest.Clock
	}

	vmSUT struct {
		*sut
		vm     *sae.SinceGenesis[hookstest.Op]
		wallet *saetest.Wallet
	}

	sutConfig struct {
		enabled        bool
		scheme         string
		log            logging.Logger
		commitInterval uint64
		avaDB          database.Database
		xdb            saetypes.ExecutionResults
		startTime      time.Time
	}
	sutOption = options.Option[sutConfig]
)

var _ saetest.Peer = (*sut)(nil)

func withEnabled(e bool) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.enabled = e
	})
}

// withScheme sets the trie database scheme.
func withScheme(scheme string) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.scheme = scheme
	})
}

// withRecordedLog records logs instead of propagating them to the test, for a
// path that deliberately warns.
func withRecordedLog() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.log = loggingtest.NewRecorder(logging.Debug)
	})
}

// withDatabase overrides the default fresh [memdb.New].
func withDatabase(db database.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.avaDB = db
	})
}

// withXDB provides an execution results database for the hooks.
func withXDB(h saetypes.ExecutionResults) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.xdb = h
	})
}

// withTime sets the SUT's clock to a specific time at startup.
//
// This is ignored by [newSUT].
func withTime(t time.Time) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.startTime = t
	})
}

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

// newSUT constructs a standalone [sut] over an otherwise untouched database.
func newSUT(t *testing.T, opts ...sutOption) *sut {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		enabled:        true,
		commitInterval: defaultCommitInterval,
		avaDB:          memdb.New(),
		xdb:            saetest.NewExecutionResultsDB(),
		log:            loggingtest.New(t, logging.Debug),
		startTime:      time.Unix(genesisTimestamp, 0),
	}, opts...)

	snowCtx := snowtest.Context(t, chainID)
	snowCtx.Log = cfg.log
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
	ethDB := saetypes.NewEthDB(cfg.avaDB)

	// The [SummaryHandler] requires the genesis block on disk, but the state
	// is deliberately committed to a throwaway trie database so that syncing
	// tests start from an empty state.
	if rawdb.ReadCanonicalHash(ethDB, 0) == (ethcommon.Hash{}) {
		dummyTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
		_, err := genesis.Commit(ethDB, dummyTrieDB)
		require.NoErrorf(t, err, "%T.Commit()", genesis)
	}

	sender := saetest.NewSender(t, nil)
	net, err := network.New(snowCtx, sender)
	require.NoError(t, err, "network.New()")

	handler, err := New(
		Config{
			DBConfig: saedb.Config{
				CommitInterval: cfg.commitInterval,
				Scheme:         cfg.scheme,
			},
			Enabled: cfg.enabled,
		},
		ethDB,
		snowCtx,
		net,
		hooks,
	)
	require.NoError(t, err, "New()")

	s := &sut{
		SummaryHandler: handler,
		Network:        net,
		cfg:            cfg,
		snowCtx:        snowCtx,
		hooks:          hooks,
		keys:           keys,
		genesis:        genesis,
		db:             ethDB,
		sender:         sender,
		clock:          clock,
	}

	s.sender.Start(t, s)
	return s
}

// requireSyncedMarkers asserts every rawdb marker [SummaryHandler.WriteSynced]
// writes. A restarting node reads these instead of replaying from genesis.
func requireSyncedMarkers(t *testing.T, client *sut, accepted, settled *blocks.Block) {
	t.Helper()

	require.Equal(t, accepted.Hash(), rawdb.ReadHeadFastBlockHash(client.db), "ReadHeadFastBlockHash()")
	require.Equal(t, settled.Hash(), rawdb.ReadFinalizedBlockHash(client.db), "ReadFinalizedBlockHash()")
	require.Equal(t, settled.Hash(), rawdb.ReadHeadBlockHash(client.db), "ReadHeadBlockHash()")
	require.Equal(t, settled.Hash(), rawdb.ReadHeadHeaderHash(client.db), "ReadHeadHeaderHash()")
	require.Equal(t, accepted.Header().Root, rawdb.ReadSnapshotRoot(client.db), "ReadSnapshotRoot()")
}

// newSyncClient returns a fresh node connected to s as a sync peer.
func (s *vmSUT) newSyncClient(t *testing.T, opts ...sutOption) *sut {
	t.Helper()

	client := newSUT(t, opts...)
	saetest.ConnectTo[saetest.Peer](t, client, s)
	return client
}

// requireVMHead asserts the VM's last accepted block.
func requireVMHead(t *testing.T, vm *vmSUT, want ids.ID) {
	t.Helper()

	got, err := vm.LastAccepted(t.Context())
	require.NoErrorf(t, err, "%T.LastAccepted()", vm)
	require.Equal(t, want, got, "last accepted block")
}

// restartAsVM starts a VM over the sut's database, as a node does once state
// sync finishes.
func (s *sut) restartAsVM(t *testing.T, at time.Time) *vmSUT {
	t.Helper()

	return newVM(t,
		withDatabase(s.cfg.avaDB),
		withXDB(saetest.CloneExecutionResultsDB(t, s.cfg.xdb)),
		withTime(at),
	)
}

func (s *sut) NodeID() ids.NodeID      { return s.snowCtx.NodeID }
func (s *sut) Sender() *saetest.Sender { return s.sender }

// syncTo emulates the behavior any user would follow, by checking the summary,
// state syncing, and marking as done. Any error in the latter two steps will be
// returned.
func (s *sut) syncTo(ctx context.Context, t *testing.T, summary *Summary) error {
	t.Helper()

	require.True(t, s.ShouldAcceptSummary(summary), "ShouldAcceptSummary()")

	if err := s.Sync(ctx, summary); err != nil {
		return err
	}

	return s.WriteSynced(summary)
}

// newVM constructs and initializes a VM and a summary handler.
func newVM(t *testing.T, opts ...sutOption) *vmSUT {
	t.Helper()

	s := newSUT(t, opts...)
	ctx := t.Context()

	genesisBytes, err := json.Marshal(s.genesis)
	require.NoErrorf(t, err, "json.Marshal(%T)", s.genesis)

	// The sut's network already registered the "p2p" metrics prefix.
	s.snowCtx.Metrics = metrics.NewPrefixGatherer()

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = ""                // no on-disk journal in tests
	vm := sae.NewSinceGenesis(s.hooks, sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			CommitInterval: s.cfg.commitInterval,
		},
		Now: s.clock.Now,
	})
	require.NoError(t, vm.Initialize(
		ctx,
		s.snowCtx,
		s.cfg.avaDB,
		genesisBytes,
		nil, // upgrade bytes
		nil, // config bytes
		nil, // fxs
		s.sender,
	), "Initialize()")
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)), "Shutdown()")
	})
	require.NoError(t, vm.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")
	require.NoError(t, vm.SetState(ctx, snow.NormalOp), "SetState(NormalOp)")

	tdb, snaps := vm.EVMState()
	require.NoError(t, RegisterHandlers(s.Network.Network, s.db, tdb, snaps, s.snowCtx.Log), "RegisterHandlers")

	return &vmSUT{
		sut:    s,
		vm:     vm,
		wallet: saetest.NewWalletWithKeyChain(s.keys, types.LatestSigner(s.genesis.Config)),
	}
}

// lastAcceptedBlock returns the VM's last accepted block.
func (s *vmSUT) lastAcceptedBlock(t *testing.T) *blocks.Block {
	t.Helper()
	ctx := t.Context()

	id, err := s.vm.LastAccepted(ctx)
	require.NoError(t, err, "LastAccepted()")
	return s.getBlock(t, id)
}

// blockAtHeight returns the VM's accepted block at the given height.
func (s *vmSUT) blockAtHeight(t *testing.T, height uint64) *blocks.Block {
	t.Helper()
	id, err := s.vm.GetBlockIDAtHeight(t.Context(), height)
	require.NoErrorf(t, err, "GetBlockIDAtHeight(%d)", height)
	return s.getBlock(t, id)
}

// getBlock returns the VM's block with the given ID.
func (s *vmSUT) getBlock(t *testing.T, id ids.ID) *blocks.Block {
	t.Helper()
	b, err := s.vm.GetBlock(t.Context(), id)
	require.NoErrorf(t, err, "GetBlock(%s)", id)
	return b
}

// acceptBlock builds, verifies, and accepts one block on top of the current
// last accepted block, waiting for it to execute and returning it. Every block
// contains a single transfer. The clock is not advanced towards the block's
// settlement; callers control settlement lag via [saetest.Clock.AdvanceToSettle].
func (s *vmSUT) acceptBlock(t *testing.T) *blocks.Block {
	t.Helper()
	ctx := t.Context()

	tx := s.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &ethcommon.Address{},
		Value:    big.NewInt(1),
		Gas:      1e6,
		GasPrice: big.NewInt(1),
	})
	backends := s.vm.GethRPCBackends()
	require.NoErrorf(t, backends.SendTx(ctx, tx), "SendTx(%#x)", tx.Hash())
	txgossiptest.WaitUntilPending(t, ctx, backends, tx)

	parent, err := s.vm.LastAccepted(ctx)
	require.NoError(t, err, "LastAccepted()")
	require.NoError(t, s.vm.SetPreference(ctx, parent, nil), "SetPreference()")

	b, err := s.vm.BuildBlock(ctx, nil)
	require.NoErrorf(t, err, "%T.BuildBlock()", s.vm)
	require.Lenf(t, b.Transactions(), 1, "%T.BuildBlock() transactions", s.vm)

	require.NoErrorf(t, s.vm.VerifyBlock(ctx, nil, b), "%T.VerifyBlock()", s.vm)
	require.NoErrorf(t, s.vm.AcceptBlock(ctx, b), "%T.AcceptBlock()", s.vm)
	require.NoError(t, b.WaitUntilExecuted(ctx), "WaitUntilExecuted()")

	return b
}

// acceptBlocks calls [vmSUT.acceptBlock] n times, advancing the clock after
// each block so that the next accepted block settles it.
func (s *vmSUT) acceptBlocks(t *testing.T, n uint64) {
	t.Helper()
	for range n {
		s.clock.AdvanceToSettle(t.Context(), t, s.acceptBlock(t))
	}
}
