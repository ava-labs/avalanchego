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

type newBlockConfig struct {
	// If prev is nil, newBlock will create the genesis block
	prev *Block
	// If vmBlock is nil, a random child of prev will be created
	vmBlock snowman.Block
	// If round is 0, it will be set to one higher than the prev's round
	round uint64
}

func newBlock(t *testing.T, config newBlockConfig) *Block {
	if config.prev == nil {
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

		block.blockTracker = newBlockTracker(block)
		return block
	}

	if config.vmBlock == nil {
		config.vmBlock = snowmantest.BuildChild(config.prev.vmBlock.(*snowmantest.Block))
	}
	if config.round == 0 {
		config.round = config.prev.metadata.Round + 1
	}

	block := &Block{
		vmBlock:      config.vmBlock,
		blockTracker: config.prev.blockTracker,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   config.round,
			Seq:     config.vmBlock.Height(),
			Prev:    config.prev.digest,
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
