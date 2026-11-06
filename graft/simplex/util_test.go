package simplex

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// stubStorage is a minimal Storage for exercising util functions.
// When err is set, GetBlock fails at seq errSeq.
type stubStorage struct {
	blocks []metadata.StateMachineBlock
	errSeq uint64
	err    error
}

func (s *stubStorage) NumBlocks() uint64 {
	return uint64(len(s.blocks))
}

func (s *stubStorage) GetBlock(seq uint64) (metadata.StateMachineBlock, *common.Finalization, error) {
	if s.err != nil && seq == s.errSeq {
		return metadata.StateMachineBlock{}, nil, s.err
	}
	return s.blocks[seq], &common.Finalization{}, nil
}

func (s *stubStorage) Index(context.Context, common.VerifiedBlock, common.Finalization) error {
	return nil
}

// nonSimplexBlock returns a pre-fork block holding only an inner block at the given height.
func nonSimplexBlock(height uint64) metadata.StateMachineBlock {
	return metadata.StateMachineBlock{InnerBlock: &testInnerBlock{Height_: height}}
}

// simplexBlock returns a non-sealing simplex block at the given epoch and seq.
func simplexBlock(epoch, seq uint64) metadata.StateMachineBlock {
	return metadata.StateMachineBlock{
		Metadata: metadata.StateMachineMetadata{
			SimplexProtocolMetadata: common.ProtocolMetadata{Epoch: epoch, Seq: seq},
		},
	}
}

// sealingBlock returns a sealing block at the given epoch and seq whose
// descriptor holds the given validator set.
func sealingBlock(epoch, seq uint64, members []metadata.NodeBLSMapping) metadata.StateMachineBlock {
	block := simplexBlock(epoch, seq)
	block.Metadata.SimplexEpochInfo.BlockValidationDescriptor = &metadata.BlockValidationDescriptor{
		AggregatedMembership: metadata.AggregatedMembership{Members: members},
	}
	return block
}

func testValidatorSet() metadata.NodeBLSMappings {
	return metadata.NodeBLSMappings{
		{NodeID: avalanchego.NodeID{1}, BLSKey: []byte{1, 2}, Weight: 1},
		{NodeID: avalanchego.NodeID{2}, BLSKey: []byte{3, 4}, Weight: 2},
	}
}

// epochTestConfig derives the last non-Simplex height from the leading
// blocks in storage that carry no Simplex metadata.
func epochTestConfig(t *testing.T, storage *stubStorage, genesisSet metadata.NodeBLSMappings) *Config {
	var lastNonSimplexHeight uint64
	for seq, block := range storage.blocks {
		if block.Metadata.SimplexProtocolMetadata.Epoch != 0 {
			break
		}
		lastNonSimplexHeight = uint64(seq)
	}
	return &Config{
		Storage:                  storage,
		PlatformChain:            newTestPChain(genesisSet),
		LastNonSimplexInnerBlock: &testInnerBlock{Height_: lastNonSimplexHeight},
		Logger:                   testutil.MakeLogger(t, 1),
	}
}

// LastBlock errors on empty storage.
func TestLastBlockEmptyStorage(t *testing.T) {
	_, _, err := LastBlock(&stubStorage{})
	require.ErrorIs(t, err, errNoGenesisBlock)
}

// LastBlock wraps GetBlock errors.
func TestLastBlockGetBlockError(t *testing.T) {
	sentinel := errors.New("disk corrupted")
	storage := &stubStorage{
		blocks: make([]metadata.StateMachineBlock, 3),
		errSeq: 2,
		err:    sentinel,
	}
	_, _, err := LastBlock(storage)
	require.ErrorIs(t, err, sentinel)
}

// LastBlock returns the block at seq numBlocks-1 and the block count.
func TestLastBlockSuccess(t *testing.T) {
	storage := &stubStorage{
		blocks: []metadata.StateMachineBlock{
			nonSimplexBlock(0),
			simplexBlock(1, 1),
		},
	}
	got, numBlocks, err := LastBlock(storage)
	require.NoError(t, err)
	require.Equal(t, uint64(2), numBlocks)
	require.Equal(t, storage.blocks[1], got)
}

// Covers each branch of getLastAcceptedEpochAndValidatorSet: genesis,
// sealing block at tip, sealing block in storage, and the error paths.
func TestGetLastAcceptedEpochAndValidatorSet(t *testing.T) {
	vdrSet := testValidatorSet()

	tests := []struct {
		name          string
		blocks        []metadata.StateMachineBlock
		expectedNodes common.Nodes
		expectedErr   error
	}{
		{
			name:          "only non-Simplex blocks starts at first Simplex height with genesis set",
			blocks:        []metadata.StateMachineBlock{nonSimplexBlock(0)},
			expectedNodes: vdrSet.Nodes(),
		},
		{
			name: "multiple non-Simplex blocks start at first Simplex height with genesis set",
			blocks: []metadata.StateMachineBlock{
				nonSimplexBlock(0),
				nonSimplexBlock(1),
				nonSimplexBlock(2),
			},
			expectedNodes: vdrSet.Nodes(),
		},
		{
			name: "sealing block at tip starts next epoch with its descriptor set",
			blocks: []metadata.StateMachineBlock{
				simplexBlock(1, 1),
				sealingBlock(1, 2, vdrSet),
			},
			expectedNodes: vdrSet.Nodes(),
		},
		{
			name: "non-sealing tip keeps its epoch, set loaded from sealing block at seq==epoch",
			blocks: []metadata.StateMachineBlock{
				nonSimplexBlock(0),
				simplexBlock(1, 1),
				sealingBlock(1, 2, vdrSet),
				simplexBlock(2, 3),
			},
			expectedNodes: vdrSet.Nodes(),
		},
		{
			name:        "empty storage errors",
			expectedErr: errNoGenesisBlock,
		},
		{
			name: "non-sealing block at the sealing seq errors",
			blocks: []metadata.StateMachineBlock{
				nonSimplexBlock(0),
				simplexBlock(1, 1),
				simplexBlock(1, 2),
				simplexBlock(2, 3),
			},
			expectedErr: errNonSealingBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &stubStorage{blocks: tt.blocks}
			config := epochTestConfig(t, storage, vdrSet)

			nodes, err := getLastAcceptedValidatorSet(config)
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedNodes, nodes)
		})
	}
}
