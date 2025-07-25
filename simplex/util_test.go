// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type newBlockConfig struct {
	// If prev is nil, newBlock will create the genesis block
	prev *Block
	// If round is 0, it will be set to one higher than the prev's round
	round uint64
}

func newBlock(t *testing.T, config newBlockConfig) *Block {
	if config.prev == nil {
		block := &Block{
			vmBlock:  &wrappedBlock{
				Block: snowmantest.Genesis,
				store: newTestVM(),
			},
			metadata: genesisMetadata,
		}
		bytes, err := block.Bytes()
		require.NoError(t, err)

		digest := computeDigest(bytes)
		block.digest = digest

		block.blockTracker = newBlockTracker(block)
		return block
	}
	if config.round == 0 {
		config.round = config.prev.metadata.Round + 1
	}

	vmBlock := snowmantest.BuildChild(config.prev.vmBlock.(*wrappedBlock).Block)
	block := &Block{
		vmBlock:      &wrappedBlock{
			Block:        vmBlock,
			store:        config.prev.vmBlock.(*wrappedBlock).store,
		},
		blockTracker: config.prev.blockTracker,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   config.round,
			Seq:     vmBlock.Height(),
			Prev:    config.prev.digest,
		},
	}

	bytes, err := block.Bytes()
	require.NoError(t, err)

	digest := computeDigest(bytes)
	block.digest = digest
	return block
}

func newTestValidatorInfo(allNodes []*testNode) map[ids.NodeID]*validators.GetValidatorOutput {
	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(allNodes))
	for _, node := range allNodes {
		vds[node.validator.NodeID] = &node.validator
	}

	return vds
}

func newEngineConfig(t *testing.T, numNodes uint64) *Config {
	return newNetworkConfigs(t, numNodes)[0]
}

type testNode struct {
	validator validators.GetValidatorOutput
	signFunc  SignFunc
}

// newNetworkConfigs creates a slice of Configs for testing purposes.
// they are initialized with a common chainID and a set of validators.
func newNetworkConfigs(t *testing.T, numNodes uint64) []*Config {
	require.Positive(t, numNodes)

	chainID := ids.GenerateTestID()

	testNodes := generateTestNodes(t, numNodes)

	configs := make([]*Config, 0, numNodes)
	for _, node := range testNodes {
		config := &Config{
			Ctx: SimplexChainContext{
				NodeID:    node.validator.NodeID,
				ChainID:   chainID,
				NetworkID: constants.UnitTestID,
			},
			Log:        logging.NoLog{},
			Validators: newTestValidatorInfo(testNodes),
			SignBLS:    node.signFunc,
			DB: memdb.New(),
		}
		configs = append(configs, config)
	}

	return configs
}

func generateTestNodes(t *testing.T, num uint64) []*testNode {
	nodes := make([]*testNode, num)
	for i := uint64(0); i < num; i++ {
		ls, err := localsigner.New()
		require.NoError(t, err)

		nodeID := ids.GenerateTestNodeID()
		nodes[i] = &testNode{
			validator: validators.GetValidatorOutput{
				NodeID:    nodeID,
				PublicKey: ls.PublicKey(),
			},
			signFunc: ls.Sign,
		}
	}
	return nodes
}

// newFinalization creates a new finalization over the blockheader, by collecting a 
// quorum of signatures from the provided configs.
func newTestFinalization(t *testing.T, configs []*Config, bh simplex.BlockHeader) simplex.Finalization {
	quorum := simplex.Quorum(len(configs))
	finalizedVotes := make([]*simplex.FinalizeVote, 0, quorum)

	for _, config := range configs[:quorum] {
		vote := simplex.ToBeSignedFinalization{
			BlockHeader: bh,
		}
		signer, _ := NewBLSAuth(config)
		sig, err := vote.Sign(&signer)
		require.NoError(t, err)
		finalizedVotes = append(finalizedVotes, &simplex.FinalizeVote{
			Finalization: vote,
			Signature:  simplex.Signature{
				Signer: config.Ctx.NodeID[:],
				Value:  sig,
			},
		})
	}

	_, verifier := NewBLSAuth(configs[0])
	sigAgg := &SignatureAggregator{verifier: &verifier}

	finalization, err := simplex.NewFinalization(configs[0].Log, sigAgg, finalizedVotes)
	require.NoError(t, err)
	return finalization
}

type testVMStore struct {
	blocks map[ids.ID]*snowmantest.Block
}

func newTestVM() *testVMStore {
	store := &testVMStore{
		blocks: map[ids.ID]*snowmantest.Block{
			snowmantest.Genesis.ID(): snowmantest.Genesis,
		},
	}
	return store
}

func setVM(s *testVMStore) *blocktest.VM {
	vm := &blocktest.VM{}
	vm.LastAcceptedF = s.lastAccepted
	vm.GetBlockF = s.getBlock
	vm.GetBlockIDAtHeightF = s.getBlockAtHeight
	return vm
}

func (v *testVMStore) getBlockAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	for _, block := range v.blocks {
		if block.Height() == height {
			return block.ID(), nil
		}
	}
	return ids.Empty, database.ErrNotFound
}

func (v *testVMStore) getBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	block, exists := v.blocks[id]
	if !exists {
		return nil, database.ErrNotFound
	}
	return block, nil
}

func (v *testVMStore) lastAccepted(ctx context.Context) (ids.ID, error) {
	// find the block with the highest height
	if len(v.blocks) == 0 {
		return ids.Empty, database.ErrNotFound
	}

	lastAcceptedID := snowmantest.Genesis.ID()
	for _, block := range v.blocks {
		if block.Height() > v.blocks[lastAcceptedID].Height() {
			lastAcceptedID = block.ID()
		}
	}

	return lastAcceptedID, nil
}

type wrappedBlock struct {
	*snowmantest.Block
	store    *testVMStore
}

func (w *wrappedBlock) Accept(ctx context.Context) error {
	if err := w.Block.Accept(ctx); err != nil {
		return err
	}

	w.store.blocks[w.ID()] = w.Block
	return nil
}
