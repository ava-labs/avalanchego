// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

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
			_, verifier := NewBLSAuth(config)
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
	genesis := newTestBlock(t, newBlockConfig{})
	genesisBytes, err := genesis.Bytes()
	require.NoError(t, err)

	vm := newTestVM()
	ctx := t.Context()
	config := newEngineConfig(t, 4)
	config.VM = vm
	_, verifier := NewBLSAuth(config)
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
	genesis := newTestBlock(t, newBlockConfig{})
	child1 := newTestBlock(t, newBlockConfig{prev: genesis})
	child2 := newTestBlock(t, newBlockConfig{prev: child1})

	configs := newNetworkConfigs(t, 4)
	configs[0].VM = genesis.vmBlock.(*wrappedBlock).vm

	_, verifier := NewBLSAuth(configs[0])
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
	genesis := newTestBlock(t, newBlockConfig{})
	child1 := newTestBlock(t, newBlockConfig{prev: genesis})
	child1Sibling := newTestBlock(t, newBlockConfig{prev: genesis})
	child2Nephew := newTestBlock(t, newBlockConfig{prev: child1Sibling})

	configs := newNetworkConfigs(t, 4)
	configs[0].VM = genesis.vmBlock.(*wrappedBlock).vm

	_, verifier := NewBLSAuth(configs[0])
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
	genesis := newTestBlock(t, newBlockConfig{})
	configs := newNetworkConfigs(t, 4)

	_, verifier := NewBLSAuth(configs[0])
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
