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

var _ = e2e.DescribePChain("[Retrieving Owner from Subnet Txs]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("owner retrieval",
		func() {
			nodeURI := e2e.Env.GetRandomNodeURI()
			pChainClient := platformvm.NewClient(nodeURI.URI)

			keychain := e2e.Env.NewKeychain(1)
			baseWallet := e2e.NewWallet(keychain, nodeURI)
			pWallet := baseWallet.P()

			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}

			var subnetID ids.ID
			var subnetTx *txs.Tx
			ginkgo.By("create a permissioned subnet", func() {
				var err error
				subnetTx, err = pWallet.IssueCreateSubnetTx(
					owner,
					e2e.WithDefaultContext(),
				)
				subnetID = subnetTx.ID()
				require.NoError(err)
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			ginkgo.By("verify owner", func() {
				pChainTxs := map[ids.ID]*txs.Tx{
					subnetID: subnetTx,
				}
				subnetOwners, err := primary.ExtractTxSubnetOwners(
					e2e.DefaultContext(),
					pChainClient,
					pChainTxs,
				)
				require.NoError(err)
				subnetOwner, found := subnetOwners[subnetID]
				require.True(found)
				require.Equal(subnetOwner, owner)
			})

			newKeychain := e2e.Env.NewKeychain(1)
			newOwner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					newKeychain.Keys[0].Address(),
				},
			}

			var transferSubnetOwnershipTx *txs.Tx
			ginkgo.By("change subnet owner", func() {
				var err error
				transferSubnetOwnershipTx, err = pWallet.IssueTransferSubnetOwnershipTx(
					subnetID,
					newOwner,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("verify new owner from CreateSubnetTx", func() {
				pChainTxs := map[ids.ID]*txs.Tx{
					subnetID: subnetTx,
				}
				subnetOwners, err := primary.ExtractTxSubnetOwners(
					e2e.DefaultContext(),
					pChainClient,
					pChainTxs,
				)
				require.NoError(err)
				subnetOwner, found := subnetOwners[subnetID]
				require.True(found)
				require.Equal(subnetOwner, newOwner)
			})

			ginkgo.By("verify new owner from TransferSubnetOwnershipTx", func() {
				pChainTxs := map[ids.ID]*txs.Tx{
					subnetID: transferSubnetOwnershipTx,
				}
				subnetOwners, err := primary.ExtractTxSubnetOwners(
					e2e.DefaultContext(),
					pChainClient,
					pChainTxs,
				)
				require.NoError(err)
				subnetOwner, found := subnetOwners[subnetID]
				require.True(found)
				require.Equal(subnetOwner, newOwner)
			})
		})
})
