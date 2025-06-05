// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/stretchr/testify/require"
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

func newTestValidatorInfo(curNodeId ids.NodeID, curNodePk *bls.PublicKey, numExtraNodes int) (*testValidatorInfo, error) {
	vds := make(map[ids.NodeID]validators.Validator, numExtraNodes+1)

	vds[curNodeId] = validators.Validator{
		PublicKey: curNodePk,
		NodeID:    curNodeId,
	}

	for range numExtraNodes {
		ls, err := localsigner.New()
		nodeID := ids.GenerateTestNodeID()
		if err != nil {
			return nil, err
		}

		validator := validators.Validator{
			PublicKey: ls.PublicKey(),
			NodeID:    nodeID,
		}
		vds[nodeID] = validator
	}

	// all we need is to generate the public keys for the validators
	return &testValidatorInfo{
		validators: vds,
	}, nil
}

// newTestEngineConfig returns a new simplex Config for testing purposes
// and also returns the LocalSigner associated with this nodes config.
func newTestEngineConfig(t *testing.T, numNodes int) (*Config, *localsigner.LocalSigner) {
	ls, err := localsigner.New()
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	simplexChainContext := SimplexChainContext{
		NodeID:   nodeID,
		ChainID:  ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
	}

	vdrs, err := newTestValidatorInfo(nodeID, ls.PublicKey(), numNodes)
	require.NoError(t, err)

	return &Config{
		Ctx:        simplexChainContext,
		Validators: vdrs,
		SignBLS:    ls.Sign,
	}, ls
}
