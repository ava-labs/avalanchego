// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

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

// TODO(alarso16): Reconsider scope of tests once full integration is added on
// consumer VMs.
type (
	sut struct {
		*Handler
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
		vm     *sae.VM
		wallet *saetest.Wallet
	}

	sutConfig struct {
		syncConfig Config
		avaDB      database.Database
		xdb        saetypes.ExecutionResults
		startTime  time.Time
	}
	sutOption = options.Option[sutConfig]
)

var _ saetest.Peer = (*sut)(nil)

func withEnabled(e bool) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.syncConfig.Enabled = e
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
		syncConfig: Config{
			Enabled: true,
			DBConfig: saedb.Config{
				CommitInterval: defaultCommitInterval,
			},
		},
		avaDB:     memdb.New(),
		xdb:       saetest.NewExecutionResultsDB(),
		startTime: time.Unix(genesisTimestamp, 0),
	}, opts...)

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

	ethDB := saetypes.NewEthDB(cfg.avaDB)
	setupGenesis(t, genesis, ethDB, nil /*triedb.Database*/)

	sender := saetest.NewSender(t, nil)
	net, err := network.New(snowCtx, sender)
	require.NoError(t, err, "network.New()")

	handler, err := New(
		cfg.syncConfig,
		ethDB,
		snowCtx,
		net,
		hooks,
	)
	require.NoError(t, err, "New()")

	s := &sut{
		Handler: handler,
		Network: net,
		cfg:     cfg,
		snowCtx: snowCtx,
		hooks:   hooks,
		keys:    keys,
		genesis: genesis,
		db:      ethDB,
		sender:  sender,
		clock:   clock,
	}

	s.sender.Start(t, s)
	return s
}

func (s *sut) NodeID() ids.NodeID      { return s.snowCtx.NodeID }
func (s *sut) Sender() *saetest.Sender { return s.sender }

func (s *sut) syncer() *Syncer {
	return s.Handler.Syncer()
}

// syncTo emulates the behavior any user would follow, by checking the summary,
// state syncing, and marking as done. Any error in the latter two steps will be
// returned.
func (s *sut) syncTo(ctx context.Context, t *testing.T, summary *Summary) error {
	t.Helper()

	syncer := s.syncer()

	require.True(t, syncer.ShouldAcceptSummary(summary), "ShouldAcceptSummary()")
	if err := syncer.Sync(ctx, summary); err != nil {
		return err
	}
	return syncer.WriteSynced(summary)
}

func (s *sut) asVM(t *testing.T, at time.Time) *vmSUT {
	t.Helper()

	return newVM(t,
		withDatabase(s.cfg.avaDB),
		withXDB(saetest.CloneExecutionResultsDB(t, s.cfg.xdb)),
		withTime(at),
	)
}

// newVM constructs and initializes a VM and a summary handler.
func newVM(t *testing.T, opts ...sutOption) *vmSUT {
	t.Helper()

	s := newSUT(t, opts...)
	ctx := t.Context()

	chainConfig := setupGenesis(t, s.genesis, s.db, triedb.NewDatabase(s.db, nil))

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = ""                // no on-disk journal in tests
	vm, err := sae.NewVM(ctx, s.hooks, sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig:      s.cfg.syncConfig.DBConfig,
		Now:           s.clock.Now,
	}, s.snowCtx, chainConfig, s.db, s.network)
	require.NoError(t, err, "NewVM()")
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)), "Shutdown()")
	})
	require.NoError(t, vm.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")
	require.NoError(t, vm.SetState(ctx, snow.NormalOp), "SetState(NormalOp)")

	tdb, snaps := vm.EVMState()
	require.NoError(t, s.Handler.RegisterServer(tdb, snaps), "RegisterServer")

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

func (s *vmSUT) compareVMs(t *testing.T, other *vmSUT) {
	t.Helper()

	lastAccepted := s.lastAcceptedBlock(t)
	otherLastAccepted := other.lastAcceptedBlock(t)
	require.Equalf(t, lastAccepted.Height(), otherLastAccepted.Height(), "last accepted block height mismatch")
	require.Equalf(t, lastAccepted.Hash(), otherLastAccepted.Hash(), "last accepted block hash mismatch")

	require.NoErrorf(t, lastAccepted.WaitUntilExecuted(t.Context()), "WaitUntilExecuted()")
	require.NoErrorf(t, otherLastAccepted.WaitUntilExecuted(t.Context()), "WaitUntilExecuted()")
	require.Equalf(t, lastAccepted.PostExecutionStateRoot(), otherLastAccepted.PostExecutionStateRoot(), "post-execution state root mismatch")
}

// setupGenesis can be used to initialize the chain state in a way friend to
// statesync. To avoid changes to the triedb, provide it as nil.
func setupGenesis(t *testing.T, genesis *core.Genesis, db ethdb.Database, tdb *triedb.Database) *params.ChainConfig {
	if tdb == nil {
		tdb = triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	}

	priorAccepted := rawdb.ReadHeadFastBlockHash(db)
	priorBlock := rawdb.ReadHeadBlockHash(db)
	priorHeader := rawdb.ReadHeadHeaderHash(db)

	config, hash, err := core.SetupGenesisBlock(db, tdb, genesis)
	require.NoErrorf(t, err, "core.SetupGenesisBlock(...): %v", err)

	// These could have been clobbered by [core.SetupGenesisBlock] if the
	// genesis state hadn't been committed.
	batch := db.NewBatch()
	if priorAccepted != (ethcommon.Hash{}) {
		rawdb.WriteHeadFastBlockHash(batch, priorAccepted)
	}
	if priorBlock != (ethcommon.Hash{}) {
		rawdb.WriteHeadBlockHash(batch, priorBlock)
	}
	if priorHeader != (ethcommon.Hash{}) {
		rawdb.WriteHeadHeaderHash(batch, priorHeader)
	}

	// [NewVM] assumes that the genesis block is "finalized", which does not
	// happen in [core.SetupGenesisBlock].
	if rawdb.ReadFinalizedBlockHash(db) == (ethcommon.Hash{}) {
		rawdb.WriteFinalizedBlockHash(batch, hash)
	}

	require.NoError(t, batch.Write(), "batch.Write()")
	return config
}
