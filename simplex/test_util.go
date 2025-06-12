// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestValidatorInfo(allVds []validators.GetValidatorOutput) map[ids.NodeID]*validators.GetValidatorOutput {
	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(allVds))
	for _, vd := range allVds {
		vds[vd.NodeID] = &vd
	}

	return vds
}

type testEngineConfig struct {
	curNode  *testValidator   // defaults to the first node
	allNodes []*testValidator // all nodes in the test. defaults to a single node
	chainID  ids.ID
	subnetID ids.ID
}

func newEngineConfig(options *testEngineConfig) (*Config, error) {
	if options == nil {
		defaultOptions, err := withNodes(1)
		if err != nil {
			return nil, err
		}
		options = defaultOptions
	}

	vdrs := newTestValidatorInfo(options.allNodes)

	simplexChainContext := SimplexChainContext{
		NodeID:   options.curNode.nodeID,
		ChainID:  options.chainID,
		SubnetID: options.subnetID,
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
