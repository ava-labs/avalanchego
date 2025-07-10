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
		SubnetID:  ids.GenerateTestID(),
		NetworkID: constants.UnitTestID,
	}

	nodeInfo := validators.GetValidatorOutput{
		NodeID:    nodeID,
		PublicKey: ls.PublicKey(),
	}

	validators := generateTestValidators(t, numNodes-1)
	validators = append(validators, nodeInfo)
	return &Config{
		Ctx:        simplexChainContext,
		Log:        logging.NoLog{},
		Validators: newTestValidators(validators),
		SignBLS:    ls.Sign,
	}
}

func generateTestValidators(t *testing.T, num uint64) []validators.GetValidatorOutput {
	vds := make([]validators.GetValidatorOutput, num)
	for i := uint64(0); i < num; i++ {
		ls, err := localsigner.New()
		require.NoError(t, err)

		nodeID := ids.GenerateTestNodeID()
		vds[i] = validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: ls.PublicKey(),
		}
	}
	return vds
	return &Config{
		Ctx:        simplexChainContext,
		Validators: newTestValidatorInfo([]validators.GetValidatorOutput{nodeInfo}),
		SignBLS:    ls.Sign,
	}, nil
}
