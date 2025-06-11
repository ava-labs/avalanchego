// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

var _ ValidatorInfo = (*testValidatorInfo)(nil)

// testValidatorInfo is a mock implementation of ValidatorInfo for testing purposes.
// it assumes all validators are in the same subnet and returns all of them for any subnetID.
type testValidatorInfo struct {
	validators map[ids.NodeID]*validators.GetValidatorOutput
}

func (t *testValidatorInfo) GetValidatorSet(
	context.Context,
	uint64,
	ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return t.validators, nil
}

func newTestValidatorInfo(nodeIds []ids.NodeID, pks []*bls.PublicKey) *testValidatorInfo {
	if len(nodeIds) != len(pks) {
		panic("nodeIds and pks must have the same length")
	}

	vds := make(map[ids.NodeID]*validators.GetValidatorOutput, len(pks))
	for i, pk := range pks {
		validator := &validators.GetValidatorOutput{
			PublicKey: pk,
			NodeID:    nodeIds[i],
		}
		vds[nodeIds[i]] = validator
	}
	// all we need is to generate the public keys for the validators
	return &testValidatorInfo{
		validators: vds,
	}
}

func newEngineConfig() (*Config, error) {
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

	return &Config{
		Ctx:        simplexChainContext,
		Validators: newTestValidatorInfo([]ids.NodeID{nodeID}, []*bls.PublicKey{ls.PublicKey()}),
		SignBLS:    ls.Sign,
	}, nil
}
