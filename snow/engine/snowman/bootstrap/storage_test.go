// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"bytes"
	"context"
	"crypto"
	"encoding/binary"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/proposervm"

	blockbuilder "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Appraiser = testAppraiser(nil)

func TestGetMissingBlockIDs(t *testing.T) {
	blocks := snowmantest.BuildChain(7)
	appraiser := makeAppraiser(blocks)

	tests := []struct {
		name               string
		blocks             []snowman.Block
		lastAcceptedHeight uint64
		expected           set.Set[ids.ID]
	}{
		{
			name:               "initially empty",
			blocks:             nil,
			lastAcceptedHeight: 0,
			expected:           nil,
		},
		{
			name:               "wants one block",
			blocks:             []snowman.Block{blocks[4]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[3].ID()),
		},
		{
			name:               "wants multiple blocks",
			blocks:             []snowman.Block{blocks[2], blocks[4]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[1].ID(), blocks[3].ID()),
		},
		{
			name:               "doesn't want last accepted block",
			blocks:             []snowman.Block{blocks[1]},
			lastAcceptedHeight: 0,
			expected:           nil,
		},
		{
			name:               "doesn't want known block",
			blocks:             []snowman.Block{blocks[2], blocks[3]},
			lastAcceptedHeight: 0,
			expected:           set.Of(blocks[1].ID()),
		},
		{
			name:               "doesn't want already accepted block",
			blocks:             []snowman.Block{blocks[1]},
			lastAcceptedHeight: 4,
			expected:           nil,
		},
		{
			name:               "doesn't underflow",
			blocks:             []snowman.Block{blocks[0]},
			lastAcceptedHeight: 0,
			expected:           nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)
			for _, blk := range test.blocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			missingBlockIDs, err := getMissingBlockIDs(
				context.Background(),
				db,
				appraiser,
				tree,
				test.lastAcceptedHeight,
			)
			require.NoError(err)
			require.Equal(test.expected, missingBlockIDs)
		})
	}
}

func TestProcess(t *testing.T) {
	blocks := snowmantest.BuildChain(7)

	tests := []struct {
		name                        string
		initialBlocks               []snowman.Block
		lastAcceptedHeight          uint64
		missingBlockIDs             set.Set[ids.ID]
		blk                         snowman.Block
		ancestors                   map[ids.ID]snowman.Block
		expectedParentID            ids.ID
		expectedShouldFetchParentID bool
		expectedMissingBlockIDs     set.Set[ids.ID]
		expectedTrackedHeights      []uint64
	}{
		{
			name:                        "add single block",
			initialBlocks:               nil,
			lastAcceptedHeight:          0,
			missingBlockIDs:             set.Of(blocks[5].ID()),
			blk:                         blocks[5],
			ancestors:                   nil,
			expectedParentID:            blocks[4].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{5},
		},
		{
			name:               "add multiple blocks",
			initialBlocks:      nil,
			lastAcceptedHeight: 0,
			missingBlockIDs:    set.Of(blocks[5].ID()),
			blk:                blocks[5],
			ancestors: map[ids.ID]snowman.Block{
				blocks[4].ID(): blocks[4],
			},
			expectedParentID:            blocks[3].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{4, 5},
		},
		{
			name:               "ignore non-consecutive blocks",
			initialBlocks:      nil,
			lastAcceptedHeight: 0,
			missingBlockIDs:    set.Of(blocks[3].ID(), blocks[5].ID()),
			blk:                blocks[5],
			ancestors: map[ids.ID]snowman.Block{
				blocks[3].ID(): blocks[3],
			},
			expectedParentID:            blocks[4].ID(),
			expectedShouldFetchParentID: true,
			expectedMissingBlockIDs:     set.Of(blocks[3].ID()),
			expectedTrackedHeights:      []uint64{5},
		},
		{
			name:                        "do not request the last accepted block",
			initialBlocks:               nil,
			lastAcceptedHeight:          2,
			missingBlockIDs:             set.Of(blocks[3].ID()),
			blk:                         blocks[3],
			ancestors:                   nil,
			expectedParentID:            ids.Empty,
			expectedShouldFetchParentID: false,
			expectedMissingBlockIDs:     set.Set[ids.ID]{},
			expectedTrackedHeights:      []uint64{3},
		},
		{
			name:                        "do not request already known block",
			initialBlocks:               []snowman.Block{blocks[2]},
			lastAcceptedHeight:          0,
			missingBlockIDs:             set.Of(blocks[1].ID(), blocks[3].ID()),
			blk:                         blocks[3],
			ancestors:                   nil,
			expectedParentID:            ids.Empty,
			expectedShouldFetchParentID: false,
			expectedMissingBlockIDs:     set.Of(blocks[1].ID()),
			expectedTrackedHeights:      []uint64{2, 3},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)
			for _, blk := range test.initialBlocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			parentID, shouldFetchParentID, err := process(
				db,
				tree,
				test.missingBlockIDs,
				test.lastAcceptedHeight,
				test.blk,
				test.ancestors,
			)
			require.NoError(err)
			require.Equal(test.expectedShouldFetchParentID, shouldFetchParentID)
			require.Equal(test.expectedParentID, parentID)
			require.Equal(test.expectedMissingBlockIDs, test.missingBlockIDs)

			require.Equal(uint64(len(test.expectedTrackedHeights)), tree.Len())
			for _, height := range test.expectedTrackedHeights {
				require.True(tree.Contains(height))
			}
		})
	}
}

func TestMeasureExecute(t *testing.T) {
	const numBlocks = 1000

	halted := &common.Halter{}
	halted.Halt(context.Background())

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	chainID := ids.GenerateTestID()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	buff := binary.AppendVarint(nil, int64(42))

	signedBlock, err := blockbuilder.Build(
		parentID,
		timestamp,
		pChainHeight,
		cert,
		buff,
		chainID,
		key,
	)
	require.NoError(t, err)

	rawBlock := signedBlock.Bytes()

	blocks := make([][]byte, numBlocks)

	for i := 0; i < numBlocks; i++ {
		blocks[i] = rawBlock
	}

	conf := proposervm.Config{
		ActivationTime:      time.Unix(0, 0),
		DurangoTime:         time.Unix(0, 0),
		MinimumPChainHeight: 0,
		Registerer:          prometheus.NewRegistry(),
	}

	innerVM := &block.TestVM{
		ParseBlockF: func(_ context.Context, rawBlock []byte) (snowman.Block, error) {
			return &snowmantest.Block{BytesV: rawBlock}, nil
		},
	}

	vm := proposervm.New(innerVM, conf)

	db := prefixdb.New([]byte{}, memdb.New())

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(cert)

	_ = vm.Initialize(context.Background(), &snow.Context{
		Log:     logging.NoLog{},
		ChainID: ids.GenerateTestID(),
	}, db, nil, nil, nil, nil, nil, nil)
	appraiseVM := &block.TestVM{
		ParseBlockF: vm.AppraiseBlock,
	}

	parseVM := &block.TestVM{
		ParseBlockF: vm.ParseBlock,
	}

	tests := []struct {
		name string
		vm   *block.TestVM
	}{
		{
			name: "appraise",
			vm:   appraiseVM,
		},
		{
			name: "parse",
			vm:   parseVM,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(t, err)

			for i, blk := range blocks {
				_, err := interval.Add(db, tree, 0, uint64(i), blk)
				require.NoError(t, err)
			}

			t1 := time.Now()
			require.NoError(t, execute(
				context.Background(),
				&common.Halter{},
				logging.NoLog{}.Info,
				db,
				test.vm,
				tree,
				numBlocks,
			))
			t.Log(time.Since(t1))
		})
	}
}

func TestExecute(t *testing.T) {
	const numBlocks = 7

	unhalted := &common.Halter{}
	halted := &common.Halter{}
	halted.Halt(context.Background())

	tests := []struct {
		name                      string
		haltable                  common.Haltable
		lastAcceptedHeight        uint64
		expectedProcessingHeights []uint64
		expectedAcceptedHeights   []uint64
	}{
		{
			name:                      "execute everything",
			haltable:                  unhalted,
			lastAcceptedHeight:        0,
			expectedProcessingHeights: nil,
			expectedAcceptedHeights:   []uint64{0, 1, 2, 3, 4, 5, 6},
		},
		{
			name:                      "do not execute blocks accepted by height",
			haltable:                  unhalted,
			lastAcceptedHeight:        3,
			expectedProcessingHeights: []uint64{1, 2, 3},
			expectedAcceptedHeights:   []uint64{0, 4, 5, 6},
		},
		{
			name:                      "do not execute blocks when halted",
			haltable:                  halted,
			lastAcceptedHeight:        0,
			expectedProcessingHeights: []uint64{1, 2, 3, 4, 5, 6},
			expectedAcceptedHeights:   []uint64{0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			tree, err := interval.NewTree(db)
			require.NoError(err)

			blocks := snowmantest.BuildChain(numBlocks)
			parser := makeAppraiser(blocks)
			for _, blk := range blocks {
				_, err := interval.Add(db, tree, 0, blk.Height(), blk.Bytes())
				require.NoError(err)
			}

			require.NoError(execute(
				context.Background(),
				test.haltable,
				logging.NoLog{}.Info,
				db,
				parser,
				tree,
				test.lastAcceptedHeight,
			))
			for _, height := range test.expectedProcessingHeights {
				require.Equal(snowtest.Undecided, blocks[height].Status)
			}
			for _, height := range test.expectedAcceptedHeights {
				require.Equal(snowtest.Accepted, blocks[height].Status)
			}

			if test.haltable.Halted() {
				return
			}

			size, err := database.Count(db)
			require.NoError(err)
			require.Zero(size)
		})
	}
}

type testAppraiser func(context.Context, []byte) (snowman.Block, error)

func (f testAppraiser) AppraiseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	return f(ctx, bytes)
}

func makeAppraiser(blocks []*snowmantest.Block) Appraiser {
	return testAppraiser(func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, block := range blocks {
			if bytes.Equal(b, block.Bytes()) {
				return block, nil
			}
		}
		return nil, database.ErrNotFound
	})
}
