// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

type testBlockConfig struct {
	vmBlock      snowman.Block
	blockTracker *blockTracker
	round        uint64
	seq          uint64
	prev         simplex.Digest
}

// newBlock constructs a random child block of the genesis. This is a helper function
// used for testing. This is helpful since otherwise we would
// need the blockDeserializer to create the block.
func newBlock(t *testing.T, config *testBlockConfig) *Block {
	genesisBlock := newGenesisBlock(t)

	if config == nil {
		config = &testBlockConfig{
			vmBlock:      snowmantest.BuildChild(snowmantest.Genesis),
			blockTracker: newBlockTracker(genesisBlock),
			round:        1,
			seq:          1,
			prev:         genesisBlock.digest,
		}
	}
	if config.blockTracker == nil {
		config.blockTracker = newBlockTracker(genesisBlock)
	}
	if config.vmBlock == nil {
		config.vmBlock = snowmantest.BuildChild(snowmantest.Genesis)
	}

	block := &Block{
		vmBlock:      config.vmBlock,
		blockTracker: config.blockTracker,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   config.round,
			Seq:     config.seq,
			Prev:    config.prev,
		},
	}

	bytes, err := block.Bytes()
	require.NoError(t, err)

	digest := computeDigest(bytes)
	block.digest = digest

	return block
}

func newGenesisBlock(t *testing.T) *Block {
	block := &Block{
		vmBlock: snowmantest.Genesis,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   0,
			Seq:     0,
		},
	}

	bytes, err := block.Bytes()
	require.NoError(t, err)

	digest := computeDigest(bytes)
	block.digest = digest

	return block
}

func newTestValidatorInfo(allVds []validators.GetValidatorOutput) map[ids.NodeID]*validators.GetValidatorOutput {
	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(allVds))
	for _, vd := range allVds {
		vds[vd.NodeID] = &vd
	}

	return vds
}

func newEngineConfig() (*Config, error) {
	ls, err := localsigner.New()
	if err != nil {
		return nil, err
	}

	nodeID := ids.GenerateTestNodeID()

	simplexChainContext := SimplexChainContext{
		NodeID:    nodeID,
		ChainID:   ids.GenerateTestID(),
		NetworkID: constants.UnitTestID,
	}

	nodeInfo := validators.GetValidatorOutput{
		NodeID:    nodeID,
		PublicKey: ls.PublicKey(),
	}

	return &Config{
		Ctx:        simplexChainContext,
		Validators: newTestValidatorInfo([]validators.GetValidatorOutput{nodeInfo}),
		SignBLS:    ls.Sign,
	}, nil
}
