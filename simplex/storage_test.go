// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

func TestStorageNew(t *testing.T) {
	ctx := context.Background()
	child := snowmantest.BuildChild(snowmantest.Genesis)

	tests := []struct {
		name           string
		vm             block.ChainVM
		expectedHeight uint64
	}{
		{
			name:           "last accepted is genesis",
			vm:             newTestVM(),
			expectedHeight: 1,
		},
		{
			name: "last accepted is not genesis",
			vm: func() block.ChainVM {
				vm := newTestVM()
				vm.blocks[child.ID()] = child
				return vm
			}(),
			expectedHeight: 2,
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

			s, err := newStorage(ctx, config, &qc, nil)
			require.NoError(t, err)
			require.Equal(t, tt.expectedHeight, s.Height())
		})
	}
}

func TestStorageRetrieve(t *testing.T) {
	genesis := newBlock(t, newBlockConfig{})
	vm := newTestVM()
	ctx := context.Background()
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
		expectedFinalization simplex.Finalization
		expectedExists       bool
	}{
		{
			name:                 "retrieve genesis block",
			seq:                  0,
			expectedBlock:        genesis,
			expectedFinalization: simplex.Finalization{},
			expectedExists:       true,
		},
		{
			name:                 "seq not found",
			seq:                  1,
			expectedBlock:        nil,
			expectedFinalization: simplex.Finalization{},
			expectedExists:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := newStorage(ctx, config, &qc, genesis.blockTracker)
			require.NoError(t, err)

			block, finalization, exists := s.Retrieve(tt.seq)
			if tt.expectedExists {
				bytes, err := block.Bytes()
				require.NoError(t, err)

				genesisBytes, err := genesis.Bytes()
				require.NoError(t, err)

				require.Equal(t, genesisBytes, bytes)
			}

			require.Equal(t, tt.expectedFinalization, finalization)
			require.Equal(t, tt.expectedExists, exists)
		})
	}
}

func TestStorageIndexFails(t *testing.T) {
	ctx := context.Background()
	genesis := newBlock(t, newBlockConfig{})
	child1 := newBlock(t, newBlockConfig{prev: genesis})
	child2 := newBlock(t, newBlockConfig{prev: child1})

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
			expectedError: errGenesisIndexed,
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

			if tt.expectedError != errGenesisIndexed {
				// ensure that the block is not retrievable
				block, finalization, exists := s.Retrieve(tt.block.BlockHeader().Seq)
				require.False(t, exists)
				require.Nil(t, block)
				require.Equal(t, simplex.Finalization{}, finalization)
			}

			// ensure that the height is not incremented
			require.Equal(t, uint64(1), s.Height())
		})
	}
}

// TestStorageIndexSuccess indexes 10 blocks and verifies that they can be retrieved.
func TestStorageIndexSuccess(t *testing.T) {
	ctx := context.Background()
	genesis := newBlock(t, newBlockConfig{})
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
		child := newBlock(t, newBlockConfig{prev: prev})
		_, err := child.Verify(ctx)
		require.NoError(t, err)

		fin := newTestFinalization(t, configs, child.BlockHeader())
		require.NoError(t, s.Index(ctx, child, fin))

		blocks = append(blocks, child)
		finalizations = append(finalizations, fin)
		prev = child
	}

	for i := 0; i <= numBlocks; i++ {
		gotBlock, gotFin, exists := s.Retrieve(uint64(i))
		require.True(t, exists)

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

	// ensure that the height is correct
	require.Equal(t, uint64(numBlocks+1), s.Height())
}
