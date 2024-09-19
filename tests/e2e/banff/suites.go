// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements tests for the banff network upgrade.
package banff

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ = ginkgo.Describe("[Banff]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can send custom assets X->P and P->X",
		func() {
			env := e2e.GetEnv(tc)
			keychain := env.NewKeychain()
			wallet := e2e.NewWallet(tc, keychain, env.GetRandomNodeURI())

			// Get the P-chain and the X-chain wallets
			pWallet := wallet.P()
			xWallet := wallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()

			// Pull out useful constants to use when issuing transactions.
			xChainID := xContext.BlockchainID
			owner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			}

			var assetID ids.ID
			tc.By("create new X-chain asset", func() {
				assetTx, err := xWallet.IssueCreateAssetTx(
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
					tc.WithDefaultContext(),
				)
				require.NoError(err)
				assetID = assetTx.ID()

				tc.Outf("{{green}}created new X-chain asset{{/}}: %s\n", assetID)
			})

			tc.By("export new X-chain asset to P-chain", func() {
				tx, err := xWallet.IssueExportTx(
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
					tc.WithDefaultContext(),
				)
				require.NoError(err)

				tc.Outf("{{green}}issued X-chain export{{/}}: %s\n", tx.ID())
			})

			tc.By("import new asset from X-chain on the P-chain", func() {
				tx, err := pWallet.IssueImportTx(
					xChainID,
					owner,
					tc.WithDefaultContext(),
				)
				require.NoError(err)

				tc.Outf("{{green}}issued P-chain import{{/}}: %s\n", tx.ID())
			})

			tc.By("export asset from P-chain to the X-chain", func() {
				tx, err := pWallet.IssueExportTx(
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
					tc.WithDefaultContext(),
				)
				require.NoError(err)

				tc.Outf("{{green}}issued P-chain export{{/}}: %s\n", tx.ID())
			})

			tc.By("import asset from P-chain on the X-chain", func() {
				tx, err := xWallet.IssueImportTx(
					constants.PlatformChainID,
					owner,
					tc.WithDefaultContext(),
				)
				require.NoError(err)

				tc.Outf("{{green}}issued X-chain import{{/}}: %s\n", tx.ID())
			})
		})
})
