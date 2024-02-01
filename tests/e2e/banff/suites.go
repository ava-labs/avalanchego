// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements tests for the banff network upgrade.
package banff

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/chain4travel/camino-node/tests"
	"github.com/chain4travel/camino-node/tests/e2e"
)

var _ = ginkgo.Describe("[Banff]", func() {
	ginkgo.It("can send custom assets X->P and P->X",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"require-network-runner",
			"xp",
			"banff",
		),
		func() {
			ginkgo.By("reload initial snapshot for test independence", func() {
				err := e2e.Env.RestoreInitialState(true /*switchOffNetworkFirst*/)
				gomega.Expect(err).Should(gomega.BeNil())
			})

			uris := e2e.Env.GetURIs()
			gomega.Expect(uris).ShouldNot(gomega.BeEmpty())

			kc := secp256k1fx.NewKeychain(genesis.EWOQKey)
			var wallet primary.Wallet
			ginkgo.By("initialize wallet", func() {
				walletURI := uris[0]

				// 5-second is enough to fetch initial UTXOs for test cluster in "primary.NewWallet"
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				var err error
				wallet, err = primary.NewWalletFromURI(ctx, walletURI, kc)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}created wallet{{/}}\n")
			})

			// Get the P-chain and the X-chain wallets
			pWallet := wallet.P()
			xWallet := wallet.X()

			// Pull out useful constants to use when issuing transactions.
			xChainID := xWallet.BlockchainID()
			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					genesis.EWOQKey.PublicKey().Address(),
				},
			}

			var assetID ids.ID
			ginkgo.By("create new X-chain asset", func() {
				var err error
				assetID, err = xWallet.IssueCreateAssetTx(
					"RnM",
					"RNM",
					9,
					map[uint32][]verify.State{
						0: {
							&secp256k1fx.TransferOutput{
								Amt:          100 * units.Schmeckle,
								OutputOwners: *owner,
							},
						},
					},
				)
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}created new X-chain asset{{/}}: %s\n", assetID)
			})

			ginkgo.By("export new X-chain asset to P-chain", func() {
				txID, err := xWallet.IssueExportTx(
					constants.PlatformChainID,
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          100 * units.Schmeckle,
								OutputOwners: *owner,
							},
						},
					},
				)
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}issued X-chain export{{/}}: %s\n", txID)
			})

			ginkgo.By("import new asset from X-chain on the P-chain", func() {
				txID, err := pWallet.IssueImportTx(xChainID, owner)
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}issued P-chain import{{/}}: %s\n", txID)
			})

			ginkgo.By("export asset from P-chain to the X-chain", func() {
				txID, err := pWallet.IssueExportTx(
					xChainID,
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: assetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt:          100 * units.Schmeckle,
								OutputOwners: *owner,
							},
						},
					},
				)
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}issued P-chain export{{/}}: %s\n", txID)
			})

			ginkgo.By("import asset from P-chain on the X-chain", func() {
				txID, err := xWallet.IssueImportTx(constants.PlatformChainID, owner)
				gomega.Expect(err).Should(gomega.BeNil())

				tests.Outf("{{green}}issued X-chain import{{/}}: %s\n", txID)
			})
		})
})
