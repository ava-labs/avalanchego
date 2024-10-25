// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
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
			_, err := pvmClient.GetProposedValidators(
				tc.DefaultContext(),
				constants.PrimaryNetworkID,
			)
			require.NoError(err)
		})
		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})
