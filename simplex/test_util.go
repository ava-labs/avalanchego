// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
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

type testValidator struct {
	nodeID ids.NodeID
	pk     *bls.PublicKey
	sign   SignFunc
}

func newTestValidators(num uint64) ([]*testValidator, error) {
	vdrs := make([]*testValidator, num)

	for i := uint64(0); i < num; i++ {
		nodeID := ids.GenerateTestNodeID()
		ls, err := localsigner.New()
		if err != nil {
			return nil, err
		}
		pk := ls.PublicKey()
		vdrs[i] = &testValidator{
			nodeID: nodeID,
			pk:     pk,
			sign:   ls.Sign,
		}
	}

	return vdrs, nil
}

func newTestValidatorInfo(all []*testValidator) *testValidatorInfo {
	vds := make(map[ids.NodeID]validators.Validator, len(all))
	for _, vd := range all {
		vds[vd.nodeID] = validators.Validator{
			NodeID:    vd.nodeID,
			PublicKey: vd.pk,
		}
	}
	// all we need is to generate the public keys for the validators
	return &testValidatorInfo{
		validators: vds,
	}
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

	return &Config{
		Ctx:        simplexChainContext,
		Validators: vdrs,
		SignBLS:    options.curNode.sign,
		Log:        logging.NoLog{}, // TODO: replace with a proper logger
	}, nil
}

func withNodes(numNodes uint64) (*testEngineConfig, error) {
	vds, err := newTestValidators(numNodes)
	if err != nil {
		return nil, err
	}

	return &testEngineConfig{
		curNode:  vds[0],
		allNodes: vds,
		chainID:  ids.GenerateTestID(),
		subnetID: ids.GenerateTestID(),
	}, nil
}
