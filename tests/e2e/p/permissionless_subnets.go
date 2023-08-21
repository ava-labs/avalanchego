// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ = e2e.DescribePChain("[Permissionless Subnets]", func() {
	ginkgo.It("subnets operations",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"xp",
			"permissionless-subnets",
		),
		func() {
			keychain := e2e.Env.NewKeychain(1)
			baseWallet := e2e.Env.NewWallet(keychain)

			nodeURI := e2e.Env.GetRandomNodeURI()
			pWallet := baseWallet.P()
			xWallet := baseWallet.X()
			xChainID := xWallet.BlockchainID()

			var validatorID ids.NodeID
			ginkgo.By("retrieving the node ID of a primary network validator", func() {
				pChainClient := platformvm.NewClient(nodeURI)
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
				validatorIDs, err := pChainClient.SampleValidators(ctx, constants.PrimaryNetworkID, 1)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
				gomega.Expect(validatorIDs).Should(gomega.HaveLen(1))
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
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				subnetTx, err := pWallet.IssueCreateSubnetTx(
					owner,
					common.WithContext(ctx),
				)
				cancel()

				subnetID = subnetTx.ID()
				gomega.Expect(subnetID, err).Should(gomega.Not(gomega.Equal(constants.PrimaryNetworkID)))
			})

			var subnetAssetID ids.ID
			ginkgo.By("create a custom asset for the permissionless subnet", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
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
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
				subnetAssetID = subnetAssetTx.ID()
			})

			ginkgo.By(fmt.Sprintf("Send 100 MegaAvax of asset %s to the P-chain", subnetAssetID), func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
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
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			ginkgo.By(fmt.Sprintf("Import the 100 MegaAvax of asset %s from the X-chain into the P-chain", subnetAssetID), func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			ginkgo.By("make subnet permissionless", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
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
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			validatorStartTime := time.Now().Add(time.Minute)
			ginkgo.By("add permissionless validator", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							Start:  uint64(validatorStartTime.Unix()),
							End:    uint64(validatorStartTime.Add(5 * time.Second).Unix()),
							Wght:   25 * units.MegaAvax,
						},
						Subnet: subnetID,
					},
					&signer.Empty{},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					&secp256k1fx.OutputOwners{},
					reward.PercentDenominator,
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})

			delegatorStartTime := validatorStartTime
			ginkgo.By("add permissionless delegator", func() {
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: validatorID,
							Start:  uint64(delegatorStartTime.Unix()),
							End:    uint64(delegatorStartTime.Add(5 * time.Second).Unix()),
							Wght:   25 * units.MegaAvax,
						},
						Subnet: subnetID,
					},
					subnetAssetID,
					&secp256k1fx.OutputOwners{},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})
		})
})
