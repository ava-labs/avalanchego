package simplex

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"
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
			name: "last accepted is genesis",
			vm: &blocktest.VM{
				LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
					return snowmantest.GenesisID, nil
				},
				GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
					if id == snowmantest.GenesisID {
						return snowmantest.Genesis, nil
					}
					return nil, errors.New("unknown block")
				},
			},
			expectedHeight: 1,
		},
		{
			name: "last accepted is not genesis",
			vm: &blocktest.VM{
				LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
					return child.IDV, nil
				},
				GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
					if id == child.IDV {
						return child, nil
					} else if id == snowmantest.GenesisID {
						return snowmantest.Genesis, nil
					}
					return nil, errors.New("unknown block")
				},
				GetBlockIDAtHeightF: func(ctx context.Context, height uint64) (ids.ID, error) {
					if height == 0 {
						return snowmantest.GenesisID, nil
					} else if height == 1 {
						return child.IDV, nil
					}
					return ids.Empty, database.ErrNotFound
				},
			},
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

	vm := &blocktest.VM{
		LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
			return snowmantest.GenesisID, nil
		},
		GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
			if id == snowmantest.GenesisID {
				return snowmantest.Genesis, nil
			}
			return nil, database.ErrNotFound
		},
	}

	// vm.

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
			ctx := context.Background()
			config := newEngineConfig(t, 1)
			_, verifier := NewBLSAuth(config)
			qc := QCDeserializer{
				verifier: &verifier,
			}

			config.VM = vm

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

func TestStorageIndex(t *testing.T) {
	ctx := context.Background()
	genesis := newBlock(t, newBlockConfig{})
	child1 := newBlock(t, newBlockConfig{prev: genesis})
	newBlock(t, newBlockConfig{prev: child1})
	child2 := newBlock(t, newBlockConfig{prev: child1})

	configs := newNetworkConfigs(t, 4)
	_, verifier := NewBLSAuth(configs[0])
	qc := QCDeserializer{
		verifier: &verifier,
	}

	vm := setVM(genesis.vmBlock.(*wrappedBlock).store)
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
			name:          "mismatched seq",
			expectedError: errUnexpectedSeq,
			block:         child1,
			finalization: func () simplex.Finalization {
				f := newTestFinalization(t, configs, child1.BlockHeader())
				f.Finalization.ProtocolMetadata.Seq = 2 // seq does not match current height
				return f
			}(),
		},
		{
			name:          "indexing too high seq",
			expectedError: errUnexpectedSeq,
			block:         child2, // index child2 before child1
			finalization: newTestFinalization(t, configs, child2.BlockHeader()),
		},
		{
			name:          "indexing before verifying",
			expectedError: errDigestNotFound,
			block:         child1,
			finalization: newTestFinalization(t, configs, child1.BlockHeader()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs[0].VM = vm

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
		})
	}
}

// indexes 10 blocks and verifies that they can be retrieved
func TestStorageIndexSuccess(t *testing.T) {
	ctx := context.Background()
	genesis := newBlock(t, newBlockConfig{})
	configs := newNetworkConfigs(t, 4)

	_, verifier := NewBLSAuth(configs[0])
	qc := QCDeserializer{
		verifier: &verifier,
	}
	
	vm := setVM(genesis.vmBlock.(*wrappedBlock).store)
	configs[0].VM = vm
	
	numBlocks := 10

	s, err := newStorage(ctx, configs[0], &qc, genesis.blockTracker)
	require.NoError(t, err)

	prevBlock := genesis
	finalizations := make([]simplex.Finalization, 0, numBlocks+1)
	blocks := make([]*Block, 0, numBlocks+1)
	finalizations = append(finalizations, simplex.Finalization{}) // genesis finalization
	blocks = append(blocks, genesis)
	for range numBlocks {
		child := newBlock(t, newBlockConfig{prev: prevBlock})

		_, err := child.Verify(ctx)
		require.NoError(t, err)

		finalization := newTestFinalization(t, configs, child.BlockHeader())
		err = s.Index(ctx, child, finalization)
		require.NoError(t, err)

		prevBlock = child
		finalizations = append(finalizations, finalization)
		blocks = append(blocks, child)
	}
	
	// perform retrieval for all blocks indexed including genesis
	for seq := range numBlocks + 1 {
		block, finalizationRetrieved, exists := s.Retrieve(uint64(seq))
		require.True(t, exists)
		bytes,err := block.Bytes()
		require.NoError(t, err)
		expectedBytes, err := blocks[seq].Bytes()
		require.NoError(t, err)

		require.Equal(t, expectedBytes, bytes)
		require.Equal(t, finalizations[seq].Finalization, finalizationRetrieved.Finalization)
	}	
}


