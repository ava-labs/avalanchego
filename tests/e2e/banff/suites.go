// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements tests for the banff network upgrade.
package banff

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

var _ = ginkgo.Describe("[Banff]", func() {
	ginkgo.It("can send custom assets X->P and P->X", func() {
		e2e.ExecuteAPITest(TestCustomAssetTransfer)
	})
})

func TestCustomAssetTransfer(
	tc tests.TestContext,
	wallet primary.Wallet,
	ownerAddress ids.ShortID,
) {
	require := require.New(tc)

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
			ownerAddress,
		},
	}

	var assetID ids.ID
	tc.By("creating new X-chain asset", func() {
		tx, err := xWallet.IssueCreateAssetTx(
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
		assetID = tx.ID()
	})

	tc.By("exporting new X-chain asset to P-chain", func() {
		_, err := xWallet.IssueExportTx(
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
	})

	tc.By("importing new asset from X-chain on the P-chain", func() {
		_, err := pWallet.IssueImportTx(
			xChainID,
			owner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)
	})

	tc.By("exporting asset from P-chain to the X-chain", func() {
		_, err := pWallet.IssueExportTx(
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
	})

	tc.By("importing asset from P-chain on the X-chain", func() {
		_, err := xWallet.IssueImportTx(
			constants.PlatformChainID,
			owner,
			tc.WithDefaultContext(),
		)
		require.NoError(err)
	})
}
