// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

type (
	sut struct {
		*SummaryHandler
		genesis *core.Genesis
		blocks  []*blocks.Block
	}
	sutConfig struct {
		enabled        *bool
		numBlocks      uint64
		commitInterval uint64
		initializeVM   bool
	}
	sutOption = options.Option[sutConfig]
)

func withNumBlocks(blocks uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.numBlocks = blocks
	})
}

func withoutInitialization() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.initializeVM = false
	})
}

func withEnabled(e *bool) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.enabled = e
	})
}

const defaultCommitInterval = 4

// newSUT constructs a VM, builds and accepts the number of
// blocks requested, each block settling the previous, and returns a fresh
// [SummaryHandler] that uses the same underlying database.
func newSUT(t *testing.T, opts ...sutOption) *sut {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		enabled:        utils.PointerTo(true),
		commitInterval: defaultCommitInterval,
		initializeVM:   true,
	}, opts...)

	logger := loggingtest.New(t, logging.Info)

	chainID := ids.GenerateTestID()
	snowCtx := snowtest.Context(t, chainID)
	snowCtx.Log = logger

	hooks := hookstest.NewStub(100e6)
	mempoolConf := legacypool.DefaultConfig
	mempoolConf.Journal = "" // no on-disk journal in tests
	saeCfg := sae.Config{
		MempoolConfig: mempoolConf,
	}
	saeCfg.DBConfig.TrieCommitInterval = cfg.commitInterval

	genesis := core.Genesis{
		Config:     saetest.ChainConfig(),
		Alloc:      saetest.MaxAllocFor(),
		Timestamp:  saeparams.TauSeconds,
		BaseFee:    big.NewInt(1),
		Difficulty: big.NewInt(0), // irrelevant but required
	}
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")

	db := memdb.New()
	reader, err := New(
		Config{
			CommitInterval: cfg.commitInterval,
			Enabled:        cfg.enabled,
		},
		snowCtx,
		saetypes.NewEthDB(db),
		genesis.ToBlock(),
	)
	require.NoError(t, err, "New()")
	if !cfg.initializeVM {
		return &sut{
			SummaryHandler: reader,
			genesis:        &genesis,
		}
	}

	vm := sae.NewSinceGenesis(hooks, saeCfg)
	ctx := logger.CancelOnError(t.Context())
	require.NoError(t, vm.Initialize(
		ctx,
		snowCtx,
		db,
		genesisBytes,
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		saetest.NewSender(t, set.Set[ids.NodeID]{}),
	), "Initialize()")
	t.Cleanup(func() {
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)), "Shutdown()")
	})

	require.NoError(t, vm.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")
	require.NoError(t, vm.SetState(ctx, snow.NormalOp), "SetState(NormalOp)")

	genesisID, err := vm.LastAccepted(ctx)
	require.NoError(t, err, "LastAccepted() [genesis]")
	genesisBlock, err := vm.GetBlock(ctx, genesisID)
	require.NoError(t, err, "GetBlock(LastAccepted()) [genesis]")

	accepted := make([]*blocks.Block, 0, cfg.numBlocks+1)
	accepted = append(accepted, genesisBlock)
	parent := genesisID
	for range cfg.numBlocks {
		require.NoError(t, vm.SetPreference(ctx, parent, nil), "SetPreference()")

		b, err := vm.BuildBlock(ctx, nil)
		require.NoErrorf(t, err, "%T.BuildBlock()", vm)

		require.NoErrorf(t, vm.VerifyBlock(ctx, nil, b), "%T.VerifyBlock()", vm)
		require.NoErrorf(t, vm.AcceptBlock(ctx, b), "%T.AcceptBlock()", vm)

		accepted = append(accepted, b)
		parent = b.ID()
	}

	return &sut{
		SummaryHandler: reader,
		genesis:        &genesis,
		blocks:         accepted,
	}
}

func TestLastAccepted(t *testing.T) {
	tests := []struct {
		name      string
		numBlocks uint64
	}{
		{
			name:      "genesis only",
			numBlocks: 0,
		},
		{
			name:      "one block",
			numBlocks: 1,
		},
		{
			name:      "past commit boundary",
			numBlocks: defaultCommitInterval + 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sut := newSUT(t, withNumBlocks(tt.numBlocks))
			last := sut.blocks[len(sut.blocks)-1]

			got, err := sut.LastAccepted(t.Context())
			require.NoError(t, err, "LastAccepted()")
			require.Equal(t, ids.ID(last.Hash()), got)
		})
	}
}

func TestBlock(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 1
	sut := newSUT(t, withNumBlocks(numBlocks))

	t.Run("GetBlockIDAtHeight", func(t *testing.T) {
		for _, b := range sut.blocks {
			got, err := sut.GetBlockIDAtHeight(t.Context(), b.Height())
			require.NoErrorf(t, err, "GetBlockIDAtHeight(%d)", b.Height())
			require.Equalf(t, ids.ID(b.Hash()), got, "GetBlockIDAtHeight(%d)", b.Height())
		}

		_, err := sut.GetBlockIDAtHeight(t.Context(), numBlocks+1)
		require.Equalf(t, database.ErrNotFound, err, "GetBlockIDAtHeight(%d)", numBlocks+1)
	})

	t.Run("GetBlock", func(t *testing.T) {
		for _, b := range sut.blocks {
			got, err := sut.GetBlock(t.Context(), ids.ID(b.Hash()))
			require.NoErrorf(t, err, "GetBlock(%s)", b.ID())
			require.Equalf(t, b.Hash(), got.Hash(), "GetBlock(%s).Hash()", b.ID())
			require.Equalf(t, b.Height(), got.Height(), "GetBlock(%s).Height()", b.ID())
		}

		_, err := sut.GetBlock(t.Context(), ids.GenerateTestID())
		require.Equal(t, database.ErrNotFound, err, "GetBlock(unknown)")
	})
}

func TestStateSummary(t *testing.T) {
	t.Parallel()

	const numBlocks = defaultCommitInterval + 1
	sut := newSUT(t, withNumBlocks(numBlocks))
	lastCommitted := sut.blocks[defaultCommitInterval].EthBlock() // last block at a commit boundary

	t.Run("GetLastStateSummary", func(t *testing.T) {
		summary, err := sut.GetLastStateSummary(t.Context())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, lastCommitted)
	})

	t.Run("GetStateSummary_at_committed_height", func(t *testing.T) {
		summary, err := sut.GetStateSummary(t.Context(), lastCommitted.NumberU64())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, lastCommitted)
	})

	t.Run("GetStateSummary_at_uncommitted_height", func(t *testing.T) {
		_, err := sut.GetStateSummary(t.Context(), numBlocks)
		require.Equal(t, database.ErrNotFound, err)
	})
}

func TestEmptyStateSummary(t *testing.T) {
	t.Parallel()

	sut := newSUT(t, withoutInitialization())
	genesis := sut.genesis.ToBlock()

	t.Run("GetLastStateSummary", func(t *testing.T) {
		summary, err := sut.GetLastStateSummary(t.Context())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, genesis)
	})

	t.Run("GetStateSummary", func(t *testing.T) {
		summary, err := sut.GetStateSummary(t.Context(), genesis.NumberU64())
		require.NoError(t, err)
		checkSummaryMatchesBlock(t, summary, genesis)
	})
}

func checkSummaryMatchesBlock(t *testing.T, summary *Summary, block *types.Block) {
	t.Helper()

	want := NewSummary(block.Hash(), block.NumberU64())
	if diff := cmp.Diff(want, summary, CmpOpt()); diff != "" {
		t.Errorf("summary mismatch (-want +got):\n%s", diff)
	}
}
