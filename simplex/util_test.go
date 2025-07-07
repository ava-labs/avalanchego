// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

// NewBlockWithDigest is a helper function that creates a new block and sets its digest.
// This is helpful since otherwise we would need the blockDeserializer to create full block.
func newBlockWithDigest(t *testing.T, vmBlock snowman.Block, tracker *blockTracker, round, seq uint64, prev simplex.Digest) *Block {
	block := &Block{
		vmBlock:      vmBlock,
		blockTracker: tracker,
		metadata: simplex.ProtocolMetadata{
			Version: 1,
			Epoch:   1,
			Round:   round,
			Seq:     seq,
			Prev:    prev,
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
