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
		name          string
		vm 		block.ChainVM
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
			qc := QCDeserializer(verifier)

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

	tests := []struct {
		name     string
		seq      uint64
		expectedBlock *Block
		expectedFinalization simplex.Finalization
		expectedExists bool
	}{
		{
			name:  "retrieve genesis block",
			seq:  0,
			expectedBlock: genesis,
			expectedFinalization: simplex.Finalization{},
			expectedExists: true,
		},
		{
			name:     "seq not found",
			seq:     1,
			expectedBlock: nil,
			expectedFinalization: simplex.Finalization{},
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			config := newEngineConfig(t, 1)
			_, verifier := NewBLSAuth(config)
			qc := QCDeserializer(verifier)
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
	// index genesis
	// index a block not the next in sequence
	// index normal

	ctx := context.Background()
	config := newEngineConfig(t, 1)
	_, verifier := NewBLSAuth(config)
	qc := QCDeserializer(verifier)
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
	config.VM = vm
	blockTracker := newBlockTracker(genesis)

	s, err := newStorage(ctx, config, &qc, blockTracker)
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
}
