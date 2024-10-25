// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
)

var _ = e2e.DescribePChain("[Proposed Validators]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("should be able to fetch proposed validators", func() {
		var (
			env     = e2e.GetEnv(tc)
			network = env.GetNetwork()
		)

		tc.By("fetching proposed validators", func() {
			pvmClient := platformvm.NewClient(env.URIs[0].URI)
			proposedVdrs, err := pvmClient.GetProposedValidators(
				tc.DefaultContext(),
				constants.PrimaryNetworkID,
			)
			require.NoError(err)

			tc.By("confirming proposed validators are the same as current validators", func() {
				// Ensure the network is configured to use the current height for the proposer
				proposerVMUseCurrentHeight, err := network.DefaultFlags.GetBoolVal(config.ProposerVMUseCurrentHeightKey, true)
				require.NoError(err)
				require.True(proposerVMUseCurrentHeight)

				proposedVdrNodes := set.NewSet[ids.NodeID](len(proposedVdrs))
				for _, vdr := range proposedVdrs {
					proposedVdrNodes.Add(vdr.NodeID)
				}
				currentVdrs, err := pvmClient.GetCurrentValidators(
					tc.DefaultContext(),
					constants.PrimaryNetworkID,
					nil,
				)
				require.NoError(err)
				currentVdrNodes := set.NewSet[ids.NodeID](len(currentVdrs))
				for _, vdr := range currentVdrs {
					currentVdrNodes.Add(vdr.NodeID)
				}
				require.Equal(proposedVdrNodes, currentVdrNodes)
			})
		})
		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
