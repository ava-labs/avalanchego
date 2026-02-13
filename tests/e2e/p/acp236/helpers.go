// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp236

import (
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

type stakingHelper struct {
	tc        *e2e.GinkgoTestContext
	require   *require.Assertions
	pvmClient *platformvm.Client
}

func (h *stakingHelper) waitForStakingCycleEnd(nodeID ids.NodeID) {
	validators, err := h.pvmClient.GetCurrentValidators(h.tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
	h.require.NoError(err)
	h.require.Len(validators, 1)
	initialWeight := validators[0].Weight

	h.tc.Eventually(func() bool {
		validators, err = h.pvmClient.GetCurrentValidators(h.tc.DefaultContext(), constants.PrimaryNetworkID, []ids.NodeID{nodeID})
		h.require.NoError(err)
		if len(validators) == 0 {
			return true
		}
		h.require.Len(validators, 1)

		return validators[0].Weight != initialWeight
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "node failed to finish staking cycle")
}

//func getStakingConfig(tc tests.TestContext, client *admin.Client) *genesis.StakingConfig {
//	require := require.New(tc)
//
//	rawNodeConfigMap, err := client.GetConfig(tc.DefaultContext())
//	require.NoError(err)
//	nodeConfigMap, ok := rawNodeConfigMap.(map[string]interface{})
//	require.True(ok)
//	stakingConfigMap, ok := nodeConfigMap["stakingConfig"].(map[string]interface{})
//	require.True(ok)
//
//	var stakingConfig genesis.StakingConfig
//	require.NoError(mapstructure.Decode(
//		stakingConfigMap,
//		&stakingConfig,
//	))
//	return &stakingConfig
//}
