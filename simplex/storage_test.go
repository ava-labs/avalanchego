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
	"github.com/stretchr/testify/require"
)

func TestStorage(t *testing.T) {
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
	ctx := context.Background()
	config := newEngineConfig(t, 1)
	_, verifier := NewBLSAuth(config)
	qc := QCDeserializer(verifier)
	genesis := newBlock(t, newBlockConfig{})

	vm := &blocktest.VM{
				LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
					return snowmantest.GenesisID, nil
				},
				GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
					if id == snowmantest.GenesisID {
						return snowmantest.Genesis, nil
					}
					return nil, errors.New("unknown block")
				},
	}
	config.VM = vm
	blockTracker := newBlockTracker(genesis)

	_, err := newStorage(ctx, config, &qc, blockTracker)
	require.NoError(t, err)

	// s.Index()

	// tests := []struct {
	// 	name     string
	// 	vm 		block.ChainVM
	// 	expected bool
	// }{
	// 	{
	// 		name:  "genesis block",
	// 		vm: &blocktest.VM{
	// 			LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
	// 				return snowmantest.GenesisID, nil
	// 			},
	// 			GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
	// 				if id == snowmantest.GenesisID {
	// 					return snowmantest.Genesis, nil
	// 				}
	// 				return nil, errors.New("unknown block")
	// 			},
	// 		},
	// 		expected: true,
	// 	},
	// 	{
	// 		name:     "seq not found",
	// 		vm: &blocktest.VM{
	// 			LastAcceptedF: func(ctx context.Context) (ids.ID, error) {
	// 				return snowmantest.GenesisID, nil
	// 			},
	// 			GetBlockF: func(ctx context.Context, id ids.ID) (snowman.Block, error) {
	// 				if id == snowmantest.GenesisID {
	// 					return snowmantest.Genesis, nil
	// 				}
	// 				return nil, errors.New("unknown block")
	// 			},
	// 		},
	// 		expected: false,
	// 	},
	// 	{
	// 		name:     "seq found",
	// 		expected: true,
	// 	},
	// }

	// for _, tt := range tests {
	// 	t.Run(tt.name, func(t *testing.T) {


	// 	})
	// }

}
func TestStorageIndex(t *testing.T) {
	// index genesis
	// index a block not the next in sequence
	// index normal
}
