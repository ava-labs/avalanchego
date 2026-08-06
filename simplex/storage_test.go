// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// putFinalizations marks each of [seqs] as a simplex block by storing a finalization for it in db.
func putFinalizations(t *testing.T, db database.KeyValueWriter, seqs ...uint64) {
	for _, seq := range seqs {
		require.NoError(t, db.Put(finalizationKey(seq), []byte("finalization")))
	}
}

func TestLocateLastNonSimplexBlock(t *testing.T) {
	tests := []struct {
		name string
		// numBlocks is the number of blocks indexed in storage.
		numBlocks uint64
		// proposerVMMaxHeight is the highest height retrievable from the proposerVM.
		proposerVMMaxHeight uint64
		// finalizedSeqs are the sequences that have a finalization, i.e. the
		// blocks that were accepted by simplex.
		finalizedSeqs []uint64
		// closeDB closes the database before the search, making every read fail.
		closeDB bool
		// expectedFound is whether a non-simplex block is expected to be located.
		expectedFound  bool
		expectedHeight uint64
		expectedErr    error
		expectedErrMsg string
	}{
		{
			name:           "no blocks in storage",
			numBlocks:      0,
			expectedErrMsg: "no blocks in storage",
		},
		{
			name:                "only genesis, simplex never activated",
			numBlocks:           1,
			proposerVMMaxHeight: 0,
			expectedFound:       true,
			expectedHeight:      0,
		},
		{
			name:                "simplex never activated",
			numBlocks:           5,
			proposerVMMaxHeight: 4,
			expectedFound:       true,
			expectedHeight:      4,
		},
		{
			name:                "simplex activated right after genesis",
			numBlocks:           5,
			proposerVMMaxHeight: 0,
			finalizedSeqs:       []uint64{1, 2, 3, 4},
			expectedFound:       true,
			expectedHeight:      0,
		},
		{
			name:                "simplex activated mid chain",
			numBlocks:           8,
			proposerVMMaxHeight: 3,
			finalizedSeqs:       []uint64{4, 5, 6, 7},
			expectedFound:       true,
			expectedHeight:      3,
		},
		{
			name:                "only the last block is a simplex block",
			numBlocks:           5,
			proposerVMMaxHeight: 3,
			finalizedSeqs:       []uint64{4},
			expectedFound:       true,
			expectedHeight:      3,
		},
		{
			// The last block isn't in the proposerVM, so it must be a simplex block,
			// yet no finalization is stored for any sequence.
			name:                "no simplex blocks found",
			numBlocks:           5,
			proposerVMMaxHeight: 0,
			expectedErrMsg:      "no simplex blocks found in storage",
		},
		{
			name:                "genesis is a simplex block",
			numBlocks:           3,
			proposerVMMaxHeight: 0,
			finalizedSeqs:       []uint64{0, 1, 2},
			expectedErrMsg:      "found simplex block at genesis block sequence number",
		},
		{
			// A state synced node may not have the last non-simplex block, as its
			// height index only covers genesis and a recent window. There is then no
			// non-simplex block to locate, which is not an error.
			name:                "last non-simplex block missing from the proposerVM",
			numBlocks:           8,
			proposerVMMaxHeight: 2,
			finalizedSeqs:       []uint64{4, 5, 6, 7},
			expectedFound:       false,
		},
		{
			name:                "database read fails",
			numBlocks:           5,
			proposerVMMaxHeight: 0,
			closeDB:             true,
			expectedErr:         database.ErrClosed,
		},
		{
			name:                "more blocks than can be searched",
			numBlocks:           math.MaxUint64,
			proposerVMMaxHeight: 0,
			expectedErrMsg:      "too many blocks in storage",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := t.Context()
			// The proposerVM holds a single chain rooted at the genesis block, up to
			// and including proposerVMMaxHeight.
			vm := newTestVM()
			for _, blk := range snowmantest.BuildDescendants(snowmantest.Genesis, int(testCase.proposerVMMaxHeight)) {
				vm.blocks[blk.ID()] = blk
			}

			db := memdb.New()
			putFinalizations(t, db, testCase.finalizedSeqs...)
			if testCase.closeDB {
				require.NoError(t, db.Close())
			}

			blk, found, err := locateLastNonSimplexBlock(ctx, snowmantest.Genesis, vm, db, logging.NoLog{}, testCase.numBlocks)

			if testCase.expectedErr != nil || testCase.expectedErrMsg != "" {
				if testCase.expectedErr != nil {
					require.ErrorIs(t, err, testCase.expectedErr)
				}
				if testCase.expectedErrMsg != "" {
					require.ErrorContains(t, err, testCase.expectedErrMsg)
				}
				require.False(t, found)
				require.Nil(t, blk)
				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.expectedFound, found)
			if !testCase.expectedFound {
				require.Nil(t, blk)
				return
			}

			require.Equal(t, testCase.expectedHeight, blk.Height())

			// The returned block must be the one the proposerVM indexes at that height.
			expectedID, err := vm.GetBlockIDAtHeight(ctx, testCase.expectedHeight)
			require.NoError(t, err)
			require.Equal(t, expectedID, blk.ID())
		})
	}
}

// TestLocateLastNonSimplexBlockUnexpectedVMError asserts that an error other than
// database.ErrNotFound from the proposerVM is reported and is not interpreted as database.ErrNotFound.
func TestLocateLastNonSimplexBlockUnexpectedVMError(t *testing.T) {
	errUnexpected := errors.New("unexpected proposerVM failure")

	tests := []struct {
		name          string
		finalizedSeqs []uint64
		// failingHeight is the height the proposerVM fails to retrieve due to an unexpected error.
		// Every other height reports database.ErrNotFound.
		failingHeight uint64
	}{
		{
			name:          "retrieving the last block fails",
			failingHeight: 4,
		},
		{
			// The last non-simplex block must not be the genesis block, as that is
			// returned without consulting the proposerVM.
			name:          "retrieving the last non-simplex block fails",
			finalizedSeqs: []uint64{2, 3, 4},
			failingHeight: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			vm := &blocktest.VM{
				GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
					if height == testCase.failingHeight {
						return ids.Empty, errUnexpected
					}
					return ids.Empty, database.ErrNotFound
				},
			}

			db := memdb.New()
			putFinalizations(t, db, testCase.finalizedSeqs...)

			blk, found, err := locateLastNonSimplexBlock(t.Context(), snowmantest.Genesis, vm, db, logging.NoLog{}, 5)
			require.ErrorIs(t, err, errUnexpected)
			require.False(t, found)
			require.Nil(t, blk)
		})
	}
}

func TestLocateLastNonSimplexBlockNoProposerVMBlocks(t *testing.T) {
	// A proposerVM that holds no blocks at all.
	vm := &blocktest.VM{
		GetBlockIDAtHeightF: func(context.Context, uint64) (ids.ID, error) {
			return ids.Empty, database.ErrNotFound
		},
	}

	tests := []struct {
		name          string
		numBlocks     uint64
		finalizedSeqs []uint64
	}{
		{
			name:      "genesis is the only block in the chain",
			numBlocks: 1,
		},
		{
			name:          "simplex activated right after genesis",
			numBlocks:     5,
			finalizedSeqs: []uint64{1, 2, 3, 4},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db := memdb.New()
			putFinalizations(t, db, testCase.finalizedSeqs...)

			blk, found, err := locateLastNonSimplexBlock(t.Context(), snowmantest.Genesis, vm, db, logging.NoLog{}, testCase.numBlocks)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, snowmantest.Genesis.ID(), blk.ID())
		})
	}
}

func TestStorageNew(t *testing.T) {
	ctx := t.Context()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	tests := []struct {
		name           string
		vm             block.ChainVM
		expectedBlocks uint64
		db             database.KeyValueReaderWriter
	}{
		{
			name:           "last accepted is genesis",
			vm:             newTestVM(),
			expectedBlocks: 1,
			db:             memdb.New(),
		},
		{
			name: "last accepted is not genesis",
			vm: func() block.ChainVM {
				vm := newTestVM()
				vm.blocks[child.ID()] = child
				return vm
			}(),
			db: func() database.KeyValueReaderWriter {
				db := memdb.New()
				finalization := newTestFinalization(t, newNetworkConfigs(t, 1), simplex.BlockHeader{
					ProtocolMetadata: simplex.ProtocolMetadata{
						Round: 1,
						Seq:   1,
					},
				})
				require.NoError(t, db.Put(finalizationKey(1), finalizationToBytes(finalization)))
				return db
			}(),
			expectedBlocks: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := newEngineConfig(t, 1)
			_, verifier, err := NewBLSAuth(config)
			require.NoError(t, err)
			qc := QCDeserializer{
				verifier: &verifier,
			}

			config.VM = tt.vm
			config.DB = tt.db
			s, err := newStorage(ctx, config, &qc, nil)
			require.NoError(t, err)
			require.Equal(t, tt.expectedBlocks, s.NumBlocks())
		})
	}
}

func TestStorageRetrieve(t *testing.T) {
	numNodes := 4
	genesis := newTestBlock(t, newBlockConfig{numNodes: uint64(numNodes)})
	genesisBytes, err := genesis.Bytes()
	require.NoError(t, err)

	vm := newTestVM()
	ctx := t.Context()
	config := newEngineConfig(t, uint64(numNodes))
	config.VM = vm
	_, verifier, err := NewBLSAuth(config)
	require.NoError(t, err)
	qc := QCDeserializer{
		verifier: &verifier,
	}

	tests := []struct {
		name                 string
		seq                  uint64
		expectedBlock        *Block
		expectedBytes        []byte
		expectedFinalization simplex.Finalization
		expectedErr          error
	}{
		{
			name:                 "retrieve genesis block",
			seq:                  0,
			expectedBlock:        genesis,
			expectedBytes:        genesisBytes,
			expectedFinalization: simplex.Finalization{},
			expectedErr:          nil,
		},
		{
			name:                 "seq not found",
			seq:                  1,
			expectedBlock:        nil,
			expectedFinalization: simplex.Finalization{},
			expectedErr:          simplex.ErrBlockNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := newStorage(ctx, config, &qc, genesis.blockTracker)
			require.NoError(t, err)

			block, finalization, err := s.Retrieve(tt.seq)
			if tt.expectedErr == nil {
				bytes, err := block.Bytes()
				require.NoError(t, err)

				require.Equal(t, tt.expectedBytes, bytes)
			}

			require.Equal(t, tt.expectedFinalization, finalization)
			require.Equal(t, tt.expectedErr, err)
		})
	}
}

func TestStorageIndexFails(t *testing.T) {
	ctx := t.Context()
	numNodes := uint64(4)
	genesis := newTestBlock(t, newBlockConfig{numNodes: numNodes})
	child1 := newTestBlock(t, newBlockConfig{prev: genesis})
	child2 := newTestBlock(t, newBlockConfig{prev: child1})

	configs := newNetworkConfigs(t, numNodes)
	configs[0].VM = genesis.vmBlock.(*wrappedBlock).vm

	_, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	qc := QCDeserializer{
		verifier: &verifier,
	}

	tests := []struct {
		name          string
		expectedError error
		finalization  simplex.Finalization
		block         *Block
	}{
		{
			name:          "index genesis block",
			expectedError: errUnexpectedSeq,
			block:         genesis,
			finalization:  simplex.Finalization{},
		},
		{
			name:          "index invalid qc",
			expectedError: errInvalidQC,
			block:         child1,
			finalization: simplex.Finalization{
				QC: nil, // no quorum certificate
				Finalization: simplex.ToBeSignedFinalization{
					BlockHeader: child1.BlockHeader(),
				},
			},
		},
		{
			name:          "mismatched digest",
			expectedError: errMismatchedDigest,
			block:         child1,
			finalization: func() simplex.Finalization {
				f := newTestFinalization(t, configs, child1.BlockHeader())
				f.Finalization.Digest = [32]byte{1, 2, 3} // set an invalid digest
				return f
			}(),
		},
		{
			name:          "indexing too high seq",
			expectedError: errUnexpectedSeq,
			block:         child2, // index child2 before child1
			finalization:  newTestFinalization(t, configs, child2.BlockHeader()),
		},
		{
			name:          "indexing before verifying",
			expectedError: errDigestNotFound,
			block:         child1,
			finalization:  newTestFinalization(t, configs, child1.BlockHeader()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := newStorage(ctx, configs[0], &qc, genesis.blockTracker)
			require.NoError(t, err)

			err = s.Index(ctx, tt.block, tt.finalization)
			require.ErrorIs(t, err, tt.expectedError)

			if tt.block.metadata.Seq != 0 {
				// ensure that the block is not retrievable
				block, finalization, err := s.Retrieve(tt.block.BlockHeader().Seq)
				require.ErrorIs(t, err, simplex.ErrBlockNotFound)
				require.Nil(t, block)
				require.Equal(t, simplex.Finalization{}, finalization)
			}

			// ensure that we haven't indexed any blocks
			require.Equal(t, uint64(1), s.NumBlocks())
		})
	}
}

// TestIndexMismatchedChild tests that the previously indexed digest matches the
// previous digest of the block being indexed.
func TestIndexMismatchedChild(t *testing.T) {
	ctx := t.Context()
	numNodes := uint64(4)

	genesis := newTestBlock(t, newBlockConfig{numNodes: numNodes})
	child1 := newTestBlock(t, newBlockConfig{prev: genesis})
	child1Sibling := newTestBlock(t, newBlockConfig{prev: genesis})
	child2Nephew := newTestBlock(t, newBlockConfig{prev: child1Sibling})

	configs := newNetworkConfigs(t, numNodes)
	configs[0].VM = genesis.vmBlock.(*wrappedBlock).vm

	_, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	qc := QCDeserializer{
		verifier: &verifier,
	}

	s, err := newStorage(ctx, configs[0], &qc, genesis.blockTracker)
	require.NoError(t, err)

	_, err = child1.Verify(ctx)
	require.NoError(t, err)
	_, err = child1Sibling.Verify(ctx)
	require.NoError(t, err)

	// Index child1
	require.NoError(t, s.Index(ctx, child1, newTestFinalization(t, configs, child1.BlockHeader())))

	_, err = child2Nephew.Verify(ctx)
	require.NoError(t, err)

	// Attempt to index the wrong child (child2Nephew) that has a different previous digest
	err = s.Index(ctx, child2Nephew, newTestFinalization(t, configs, child2Nephew.BlockHeader()))
	require.ErrorIs(t, err, errMismatchedPrevDigest)
}

// TestStorageIndexSuccess indexes 10 blocks and verifies that they can be retrieved.
func TestStorageIndexSuccess(t *testing.T) {
	ctx := t.Context()
	numNodes := uint64(4)
	genesis := newTestBlock(t, newBlockConfig{numNodes: 4})
	configs := newNetworkConfigs(t, numNodes)

	_, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	qc := QCDeserializer{verifier: &verifier}
	configs[0].VM = genesis.vmBlock.(*wrappedBlock).vm

	s, err := newStorage(ctx, configs[0], &qc, genesis.blockTracker)
	require.NoError(t, err)

	numBlocks := 10
	blocks := make([]*Block, 0, numBlocks+1)
	finalizations := make([]simplex.Finalization, 0, numBlocks+1)

	blocks = append(blocks, genesis)
	finalizations = append(finalizations, simplex.Finalization{})

	prev := genesis
	for i := 0; i < numBlocks; i++ {
		child := newTestBlock(t, newBlockConfig{prev: prev})
		_, err := child.Verify(ctx)
		require.NoError(t, err)

		fin := newTestFinalization(t, configs, child.BlockHeader())
		require.NoError(t, s.Index(ctx, child, fin))

		blocks = append(blocks, child)
		finalizations = append(finalizations, fin)
		prev = child
	}

	for i := 0; i <= numBlocks; i++ {
		gotBlock, gotFin, err := s.Retrieve(uint64(i))
		require.NoError(t, err)

		expectedBytes, err := blocks[i].Bytes()
		require.NoError(t, err)

		gotBytes, err := gotBlock.Bytes()
		require.NoError(t, err)

		require.Equal(t, expectedBytes, gotBytes)
		require.Equal(t, finalizations[i].Finalization, gotFin.Finalization)

		// verify that the blocks were also accepted in the VM
		accepted := blocks[i].vmBlock.(*wrappedBlock).Status
		require.Equal(t, snowtest.Accepted, accepted)
	}

	require.Equal(t, uint64(numBlocks+1), s.NumBlocks())
}
