// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

type testConfig struct {
	Config
	Nodes []*testSigner
}

type testSigner struct {
	validators.GetValidatorOutput
	sign SignFunc
}

func newTestValidators(allVds []*testSigner) map[ids.NodeID]*validators.GetValidatorOutput {
	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(allVds))
	for _, vd := range allVds {
		vds[vd.NodeID] = &validators.GetValidatorOutput{
			NodeID:    vd.NodeID,
			PublicKey: vd.PublicKey,
			Weight:    1, // Default weight for testing
		}
	}

	return vds
}

// func newEngineConfig(t *testing.T, numNodes uint64) *testConfig {
// 	if numNodes == 0 {
// 		panic("numNodes must be greater than 0")
// 	}

// 	ls, err := localsigner.New()
// 	require.NoError(t, err)

// 	nodeID := ids.GenerateTestNodeID()

// 	simplexChainContext := SimplexChainContext{
// 		NodeID:   nodeID,
// 		ChainID:  ids.GenerateTestID(),
// 		SubnetID: ids.GenerateTestID(),
// 	}

// 	nodeInfo := &testSigner{
// 		GetValidatorOutput: validators.GetValidatorOutput{
// 			NodeID:    nodeID,
// 			PublicKey: ls.PublicKey(),
// 		},
// 		sign: ls.Sign,
// 	}

// 	nodes := generateTestValidators(t, numNodes-1)
// 	nodes = append([]*testSigner{nodeInfo}, nodes...)

// 	return &testConfig{
// 		Config: Config{
// 			Ctx:        simplexChainContext,
// 			Validators: newTestValidators(nodes),
// 			SignBLS:    ls.Sign,
// 		},
// 		Nodes: nodes,
// 	}
// }

func generateTestValidators(t *testing.T, num uint64) []*testSigner {
	vds := make([]*testSigner, num)
	for i := uint64(0); i < num; i++ {
		ls, err := localsigner.New()
		require.NoError(t, err)

		nodeID := ids.GenerateTestNodeID()
		vds[i] = &testSigner{
			GetValidatorOutput: validators.GetValidatorOutput{
				NodeID:    nodeID,
				PublicKey: ls.PublicKey(),
			},
			sign: ls.Sign,
		}
	}
	return vds
}
