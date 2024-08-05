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
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("subnets operations",
		func() {
			env := e2e.GetEnv(tc)

			nodeURI := env.GetRandomNodeURI()

			keychain := env.NewKeychain(1)
			baseWallet := e2e.NewWallet(tc, keychain, nodeURI)

			pWallet := baseWallet.P()
			xWallet := baseWallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()
			xChainID := xContext.BlockchainID

			var validatorID ids.NodeID
			tc.By("retrieving the node ID of a primary network validator", func() {
				pChainClient := platformvm.NewClient(nodeURI.URI)
				validatorIDs, err := pChainClient.SampleValidators(tc.DefaultContext(), constants.PrimaryNetworkID, 1)
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
			tc.By("create a permissioned subnet", func() {
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					tc.WithDefaultContext(),
				)

				subnetID = subnetTx.ID()
				require.NoError(err)
				require.NotEqual(subnetID, constants.PrimaryNetworkID)
			})

			var subnetAssetID ids.ID
			tc.By("create a custom asset for the permissionless subnet", func() {
				subnetAssetTx, err := xWallet.IssueCreateAssetTx(
					"RnM",
					"RNM",
					9,
					map[uint32][]verify.State{
						0: {
							&secp256k1fx.TransferOutput{
								Amt:          100 * units.MegaAvax,
								OutputOwners: *owner,
							},
						},
					},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
				subnetAssetID = subnetAssetTx.ID()
			})

			tc.By(fmt.Sprintf("Send 100 MegaAvax of asset %s to the P-chain", subnetAssetID), func() {
				_, err := xWallet.IssueExportTx(
					constants.PlatformChainID,
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: subnetAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          100 * units.MegaAvax,
								OutputOwners: *owner,
							},
						},
					},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By(fmt.Sprintf("Import the 100 MegaAvax of asset %s from the X-chain into the P-chain", subnetAssetID), func() {
				_, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By("make subnet permissionless", func() {
				_, err := pWallet.IssueTransformSubnetTx(
					subnetID,
					subnetAssetID,
					50*units.MegaAvax,
					100*units.MegaAvax,
					reward.PercentDenominator,
					reward.PercentDenominator,
					1,
					100*units.MegaAvax,
					time.Second,
					365*24*time.Hour,
					0,
					1,
					5,
					.80*reward.PercentDenominator,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			endTime := time.Now().Add(time.Minute)
			tc.By("add permissionless validator", func() {
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   25 * units.MegaAvax,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})

			tc.By("add permissionless delegator", func() {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							End:    uint64(endTime.Unix()),
							Wght:   25 * units.MegaAvax,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					tc.WithDefaultContext(),
				)
				require.NoError(err)
			})
		})
})
