// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = e2e.DescribePChain("[P-Chain Wallet]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should support retrieving subnet owners", func() {
		env := e2e.GetEnv(tc)

		nodeURI := env.GetRandomNodeURI()
		pChainClient := platformvm.NewClient(nodeURI.URI)

		keychain := env.NewKeychain()
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		pWallet := baseWallet.P()

		owner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		}

		tc.By("creating a permissioned subnet")
		subnetTx, err := pWallet.IssueCreateSubnetTx(
			owner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)
		subnetID := subnetTx.ID()
		require.NotEqual(subnetID, constants.PrimaryNetworkID)

		tc.By("verifying owner", func() {
			subnetOwners, err := platformvm.GetSubnetOwners(
				pChainClient,
				tc.DefaultContext(),
				subnetID,
			)
			require.NoError(err)
			subnetOwnerInterface, found := subnetOwners[subnetID]
			require.True(found)
			subnetOwner, ok := subnetOwnerInterface.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(owner.Locktime, subnetOwner.Locktime)
			require.Equal(owner.Threshold, subnetOwner.Threshold)
			require.Equal(owner.Addrs, subnetOwner.Addrs)
		})

		newOwnerKey := e2e.NewPrivateKey(tc)
		newOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				newOwnerKey.Address(),
			},
		}

		tc.By("changing subnet owner")
		_, err = pWallet.IssueTransferSubnetOwnershipTx(
			subnetID,
			newOwner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.By("verifying new owner", func() {
			subnetOwners, err := platformvm.GetSubnetOwners(
				pChainClient,
				tc.DefaultContext(),
				subnetID,
			)
			require.NoError(err)
			subnetOwnerInterface, found := subnetOwners[subnetID]
			require.True(found)
			subnetOwner, ok := subnetOwnerInterface.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(newOwner.Locktime, subnetOwner.Locktime)
			require.Equal(newOwner.Threshold, subnetOwner.Threshold)
			require.Equal(newOwner.Addrs, subnetOwner.Addrs)
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
