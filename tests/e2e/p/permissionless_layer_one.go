// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Permissionless L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("e2e flow",
		func() {
			env := e2e.GetEnv(tc)

			nodeURI := env.GetRandomNodeURI()

			infoClient := info.NewClient(nodeURI.URI)

			tc.By("get upgrade config")
			upgrades, err := infoClient.Upgrades(tc.DefaultContext())
			require.NoError(err)

			now := time.Now()
			if !upgrades.IsEtnaActivated(now) {
				ginkgo.Skip("Etna is not activated. Permissionless L1s are enabled post-Etna, skipping test.")
			}

			keychain := env.NewKeychain(1)
			baseWallet := e2e.NewWallet(tc, keychain, nodeURI)

			pWallet := baseWallet.P()
			pClient := platformvm.NewClient(nodeURI.URI)

			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}

			var subnetID ids.ID
			tc.By("create a permissioned subnet", func() {
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					tc.WithDefaultContext(),
				)

				subnetID = subnetTx.ID()
				require.NoError(err)
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			chainID := ids.GenerateTestID()
			address := []byte{'a', 'd', 'd', 'r', 'e', 's', 's'}
			tc.By("convert subnet to permissionless L1", func() {
				convertSubnetTx, err := pWallet.IssueConvertSubnetTx(
					subnetID,
					chainID,
					address,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
				require.NoError(platformvm.AwaitTxAccepted(pClient, tc.DefaultContext(), convertSubnetTx.ID(), 100*time.Millisecond))
			})
		})
})
