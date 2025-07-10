// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
)

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
