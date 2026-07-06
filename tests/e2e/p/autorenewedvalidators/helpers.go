// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package autorenewedvalidators

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/upgrade"
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

func requireHeliconActivated(
	tc *e2e.GinkgoTestContext,
	assertions *require.Assertions,
	infoClient *info.Client,
) {
	upgrades, err := infoClient.Upgrades(tc.DefaultContext())
	assertions.NoError(err)

	if upgrades.HeliconTime.Equal(upgrade.UnscheduledActivationTime) {
		ginkgo.Skip("skipping test because Helicon isn't scheduled")
	}

	if wait := time.Until(upgrades.HeliconTime); wait > 0 {
		tc.By("waiting for Helicon activation", func() {
			time.Sleep(wait)
		})
	}

	tc.Eventually(func() bool {
		upgrades, err = infoClient.Upgrades(tc.DefaultContext())
		assertions.NoError(err)

		return upgrades.IsHeliconActivated(time.Now())
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "Helicon should have activated")
}
