// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

var _ ValidatorInfo = (*testValidatorInfo)(nil)

// testValidatorInfo is a mock implementation of ValidatorInfo for testing purposes.
// it assumes all validators are in the same subnet and returns all of them for any subnetID.
type testValidatorInfo struct {
	validators map[ids.NodeID]validators.Validator
}

func (v *testValidatorInfo) GetValidatorIDs(_ ids.ID) []ids.NodeID {
	if v.validators == nil {
		return nil
	}

	ids := make([]ids.NodeID, 0, len(v.validators))
	for id := range v.validators {
		ids = append(ids, id)
	}
	return ids
}

func (v *testValidatorInfo) GetValidator(_ ids.ID, nodeID ids.NodeID) (*validators.Validator, bool) {
	if v.validators == nil {
		return nil, false
	}

	val, exists := v.validators[nodeID]
	if !exists {
		return nil, false
	}
	return &val, true
}

func newTestValidatorInfo(nodeIds []ids.NodeID, pks []*bls.PublicKey) *testValidatorInfo {
	if len(nodeIds) != len(pks) {
		panic("nodeIds and pks must have the same length")
	}

	vds := make(map[ids.NodeID]validators.Validator, len(pks))
	for i, pk := range pks {
		validator := validators.Validator{
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
