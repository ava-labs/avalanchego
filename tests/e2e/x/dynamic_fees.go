// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("[Dynamic Fees]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should ensure that the dynamic multifees are affected by load", func() {
		customDynamicFeesConfig := commonfees.DynamicFeesConfig{
			InitialFeeRate:     commonfees.Dimensions{1, 2, 3, 4},
			MinFeeRate:         commonfees.Dimensions{1, 1, 1, 1},
			UpdateCoefficient:  commonfees.Dimensions{1, 1, 1, 1},
			BlockMaxComplexity: commonfees.Max,

			// BlockUnitsTarget are set to cause an increase of fees while simple transactions are issued
			BlockTargetComplexityRate: commonfees.Dimensions{300, 80, 150, 800},
		}

		ginkgo.By("creating a new private network to ensure isolation from other tests")
		privateNetwork := &tmpnet.Network{
			Owner: "avalanchego-e2e-dynamic-fees",
			ChainConfigs: map[string]tmpnet.FlagsMap{
				"X": {
					"dynamic-fees-config": customDynamicFeesConfig,
				},
			},
		}
		e2e.Env.StartPrivateNetwork(privateNetwork)

		ginkgo.By("setup a wallet and a X-chain client")
		node := privateNetwork.Nodes[0]
		nodeURI := tmpnet.NodeURI{
			NodeID: node.NodeID,
			URI:    node.URI,
		}
		keychain := secp256k1fx.NewKeychain(privateNetwork.PreFundedKeys...)
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		xWallet := baseWallet.X()
		xChainClient := avm.NewClient(nodeURI.URI, "X")

		// retrieve initial balances
		xBuilder := xWallet.Builder()
		xContext := xBuilder.Context()
		avaxAssetID := xContext.AVAXAssetID
		xBalances, err := xWallet.Builder().GetFTBalance()
		require.NoError(err)
		xStartBalance := xBalances[avaxAssetID]
		tests.Outf("{{blue}} X-chain initial balance: %d {{/}}\n", xStartBalance)

		ginkgo.By("checking that initial fee values match with configured ones", func() {
			currFeeRates, _, err := xChainClient.GetFeeRates(e2e.DefaultContext())
			require.NoError(err)
			require.Equal(customDynamicFeesConfig.InitialFeeRate, currFeeRates)
		})

		ginkgo.By("issue expensive transactions so to increase the fee rates to be paid for accepting the transactons",
			func() {
				currFeeRates := commonfees.Empty

				ginkgo.By("repeatedly change the permissioned subnet owner to increase fee rates", func() {
					txsCount := 10
					for i := 0; i < txsCount; i++ {
						owner := secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								keychain.Keys[1].Address(),
							},
						}

						_, err := xWallet.IssueCreateAssetTx(
							"HI",
							"HI",
							byte(txsCount),
							map[uint32][]verify.State{
								0: {
									&secp256k1fx.TransferOutput{
										Amt:          units.Schmeckle,
										OutputOwners: owner,
									},
								},
							},
						)
						require.NoError(err)

						updatedFeeRates, _, err := xChainClient.GetFeeRates(e2e.DefaultContext())
						require.NoError(err)
						tests.Outf("{{blue}} current fee rates: %v {{/}}\n", updatedFeeRates)

						ginkgo.By("check that fee rates components have increased")
						require.True(commonfees.Compare(currFeeRates, updatedFeeRates))
						currFeeRates = updatedFeeRates
					}
				})

				ginkgo.By("wait for the fee rates to decrease", func() {
					initialFeeRates := currFeeRates
					e2e.Eventually(func() bool {
						var err error
						_, currFeeRates, err = xChainClient.GetFeeRates(e2e.DefaultContext())
						require.NoError(err)
						tests.Outf("{{blue}} next fee rates: %v {{/}}\n", currFeeRates)
						return commonfees.Compare(initialFeeRates, currFeeRates)
					}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
					tests.Outf("\n{{blue}}fee rates have decreased to %v{{/}}\n", currFeeRates)
				})
			},
		)
	})
})
