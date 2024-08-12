// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[P-Chain Wallet]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should support retrieving subnet owners", func() {
		env := e2e.GetEnv(tc)

		nodeURI := env.GetRandomNodeURI()
		pChainClient := platformvm.NewClient(nodeURI.URI)

		keychain := env.NewKeychain(1)
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
		subnetID := subnetTx.ID()
		require.NoError(err)
		require.NotEqual(subnetID, constants.PrimaryNetworkID)

		tc.By("verifying owner", func() {
			pChainTxs := map[ids.ID]*txs.Tx{
				subnetID: subnetTx,
			}
			subnetOwners, err := primary.ExtractTxSubnetOwners(
				tc.DefaultContext(),
				pChainClient,
				pChainTxs,
			)
			require.NoError(err)
			subnetOwnerI, found := subnetOwners[subnetID]
			require.True(found)
			subnetOwner, ok := subnetOwnerI.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(owner.Locktime, subnetOwner.Locktime)
			require.Equal(owner.Threshold, subnetOwner.Threshold)
			require.Equal(owner.Addrs, subnetOwner.Addrs)
		})

		newKeychain := env.NewKeychain(1)
		newOwner := &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				newKeychain.Keys[0].Address(),
			},
		}

		tc.By("changing subnet owner")
		transferSubnetOwnershipTx, err := pWallet.IssueTransferSubnetOwnershipTx(
			subnetID,
			newOwner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)

		tc.By("verifying new owner from CreateSubnetTx", func() {
			pChainTxs := map[ids.ID]*txs.Tx{
				subnetID: subnetTx,
			}
			subnetOwners, err := primary.ExtractTxSubnetOwners(
				tc.DefaultContext(),
				pChainClient,
				pChainTxs,
			)
			require.NoError(err)
			subnetOwnerI, found := subnetOwners[subnetID]
			require.True(found)
			subnetOwner, ok := subnetOwnerI.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(newOwner.Locktime, subnetOwner.Locktime)
			require.Equal(newOwner.Threshold, subnetOwner.Threshold)
			require.Equal(newOwner.Addrs, subnetOwner.Addrs)
		})

		tc.By("verifying new owner from TransferSubnetOwnershipTx", func() {
			pChainTxs := map[ids.ID]*txs.Tx{
				subnetID: transferSubnetOwnershipTx,
			}
			subnetOwners, err := primary.ExtractTxSubnetOwners(
				tc.DefaultContext(),
				pChainClient,
				pChainTxs,
			)
			require.NoError(err)
			subnetOwnerI, found := subnetOwners[subnetID]
			require.True(found)
			subnetOwner, ok := subnetOwnerI.(*secp256k1fx.OutputOwners)
			require.True(ok)
			require.Equal(newOwner.Locktime, subnetOwner.Locktime)
			require.Equal(newOwner.Threshold, subnetOwner.Threshold)
			require.Equal(newOwner.Addrs, subnetOwner.Addrs)
		})
	})
})
