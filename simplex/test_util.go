// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func newTestValidators(allVds []validators.GetValidatorOutput) map[ids.NodeID]*validators.GetValidatorOutput {
	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(allVds))
	for _, vd := range allVds {
		vds[vd.NodeID] = &vd
	}

	return vds
}

func newEngineConfig(numNodes uint64) (*Config, error) {
	if numNodes == 0 {
		numNodes = 1 // Ensure at least one node for testing
	}
  
	ls, err := localsigner.New()
	if err != nil {
		return nil, err
	}

	nodeID := ids.GenerateTestNodeID()

	simplexChainContext := SimplexChainContext{
		NodeID:   nodeID,
		ChainID:  ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
	}

	nodeInfo := validators.GetValidatorOutput{
		NodeID:    nodeID,
		PublicKey: ls.PublicKey(),
	}

	validators, err := generateTestValidators(numNodes - 1)
	if err != nil {
		return nil, err
	}

	return &Config{
		Ctx:        simplexChainContext,
		Validators: newTestValidators(append(validators, nodeInfo)),
		SignBLS:    ls.Sign,
	}, nil
}

func generateTestValidators(num uint64) ([]validators.GetValidatorOutput, error) {
	vds := make([]validators.GetValidatorOutput, num)
	for i := uint64(0); i < num; i++ {
		ls, err := localsigner.New()
		if err != nil {
			return nil, err
		}

		nodeID := ids.GenerateTestNodeID()
		vds[i] = validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: ls.PublicKey(),
		}
	}
	return vds, nil
}

