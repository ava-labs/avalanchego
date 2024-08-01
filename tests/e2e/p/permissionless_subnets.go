// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = e2e.DescribePChain("[Permissionless Subnets]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("subnets operations",
		func() {
			nodeURI := e2e.Env.GetRandomNodeURI()

			keychain := e2e.Env.NewKeychain()
			baseWallet := e2e.NewWallet(keychain, nodeURI)

			pWallet := baseWallet.P()
			xWallet := baseWallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()
			xChainID := xContext.BlockchainID

			var validatorID ids.NodeID
			ginkgo.By("retrieving the node ID of a primary network validator", func() {
				pChainClient := platformvm.NewClient(nodeURI.URI)
				validatorIDs, err := pChainClient.SampleValidators(e2e.DefaultContext(), constants.PrimaryNetworkID, 1)
				require.NoError(err)
				validatorID = validatorIDs[0]
			})

			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}

			var subnetID ids.ID
			ginkgo.By("create a permissioned subnet", func() {
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					e2e.WithDefaultContext(),
				)

				require.NoError(err)
				subnetID = subnetTx.ID()
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			validatorWeight := units.Avax
			initialSupply := 2 * validatorWeight
			maxSupply := 2 * initialSupply

			var subnetAssetID ids.ID
			ginkgo.By("create a custom asset for the permissionless subnet", func() {
				subnetAssetTx, err := xWallet.IssueCreateAssetTx(
					"RnM",
					"RNM",
					9,
					map[uint32][]verify.State{
						0: {
							&secp256k1fx.TransferOutput{
								Amt:          maxSupply,
								OutputOwners: *owner,
							},
						},
					},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
				subnetAssetID = subnetAssetTx.ID()
			})

			ginkgo.By(fmt.Sprintf("Send 4 Avax of asset %s to the P-chain", subnetAssetID), func() {
				_, err := xWallet.IssueExportTx(
					constants.PlatformChainID,
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: subnetAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          maxSupply,
								OutputOwners: *owner,
							},
						},
					},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By(fmt.Sprintf("Import the 4 Avax of asset %s from the X-chain into the P-chain", subnetAssetID), func() {
				_, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("make subnet permissionless", func() {
				_, err := pWallet.IssueTransformSubnetTx(
					subnetID,
					subnetAssetID,
					initialSupply,
					maxSupply,
					reward.PercentDenominator,
					reward.PercentDenominator,
					1,
					maxSupply,
					time.Second,
					365*24*time.Hour,
					0,
					1,
					5,
					.80*reward.PercentDenominator,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			endTime := time.Now().Add(time.Minute)
			ginkgo.By("add permissionless validator", func() {
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   validatorWeight,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("add permissionless delegator", func() {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   validatorWeight,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})
		})
})
