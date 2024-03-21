// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"math"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("[Dynamic Fees]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should ensure that the dynamic multifees are affected by load", func() {
		customDynamicFeesConfig := commonfees.DynamicFeesConfig{
			InitialUnitFees:   commonfees.Dimensions{1, 2, 3, 4},
			MinUnitFees:       commonfees.Dimensions{1, 1, 1, 1},
			UpdateCoefficient: commonfees.Dimensions{1, 1, 1, 1},
			BlockUnitsCap:     commonfees.Dimensions{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64},

			// BlockUnitsTarget are set to cause an increase of fees while simple transactions are issued
			BlockUnitsTarget: commonfees.Dimensions{1_000, 1_000, 1_000, 3_000},
		}

		ginkgo.By("creating a new private network to ensure isolation from other tests")
		privateNetwork := &tmpnet.Network{
			Owner: "avalanchego-e2e-dynamic-fees",
			ChainConfigs: map[string]tmpnet.FlagsMap{
				"P": {
					"dynamic-fees-config": customDynamicFeesConfig,
				},
			},
		}
		e2e.Env.StartPrivateNetwork(privateNetwork)

		ginkgo.By("setup a wallet and a P-chain client")
		node := privateNetwork.Nodes[0]
		nodeURI := tmpnet.NodeURI{
			NodeID: node.NodeID,
			URI:    node.URI,
		}
		keychain := secp256k1fx.NewKeychain(privateNetwork.PreFundedKeys...)
		baseWallet := e2e.NewWallet(keychain, nodeURI)
		pWallet := baseWallet.P()
		pChainClient := platformvm.NewClient(nodeURI.URI)

		// retrieve initial balances
		pBuilder := pWallet.Builder()
		pContext := pBuilder.Context()
		avaxAssetID := pContext.AVAXAssetID
		pBalances, err := pWallet.Builder().GetBalance()
		require.NoError(err)
		pStartBalance := pBalances[avaxAssetID]
		tests.Outf("{{blue}} P-chain balance before P->X export: %d {{/}}\n", pStartBalance)

		ginkgo.By("checking that initial fee values match with configured ones", func() {
			currUnitFees, _, err := pChainClient.GetUnitFees(e2e.DefaultContext())
			require.NoError(err)
			require.Equal(customDynamicFeesConfig.InitialUnitFees, currUnitFees)
		})

		ginkgo.By("issue expensive transactions so to increase the unit fees to be paid for accepting the transactons",
			func() {
				initialOwner := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						keychain.Keys[0].Address(),
					},
				}

				var subnetID ids.ID
				ginkgo.By("create a permissioned subnet", func() {
					subnetTx, err := pWallet.IssueCreateSubnetTx(
						initialOwner,
						e2e.WithDefaultContext(),
					)
					require.NoError(err)

					subnetID = subnetTx.ID()
				})

				currUnitFees, _, err := pChainClient.GetUnitFees(e2e.DefaultContext())
				require.NoError(err)
				tests.Outf("{{blue}} current unit fees: %v {{/}}\n", currUnitFees)

				ginkgo.By("repeatedly change the permissioned subnet owner to increase unit fees", func() {
					txsCount := 10
					for i := 0; i < txsCount; i++ {
						nextOwner := &secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								keychain.Keys[1].Address(),
							},
						}

						_, err = pWallet.IssueTransferSubnetOwnershipTx(
							subnetID,
							nextOwner,
							e2e.WithDefaultContext(),
						)
						require.NoError(err)

						updatedUnitFees, _, err := pChainClient.GetUnitFees(e2e.DefaultContext())
						require.NoError(err)
						tests.Outf("{{blue}} current unit fees: %v {{/}}\n", updatedUnitFees)

						ginkgo.By("check that unit fees components have increased")
						require.True(commonfees.Compare(currUnitFees, updatedUnitFees))
						currUnitFees = updatedUnitFees
					}
				})

				ginkgo.By("wait for the unit fees to decrease", func() {
					initialUnitFees := currUnitFees
					e2e.Eventually(func() bool {
						var err error
						_, currUnitFees, err = pChainClient.GetUnitFees(e2e.DefaultContext())
						require.NoError(err)
						tests.Outf("{{blue}} next unit fees: %v {{/}}\n", currUnitFees)
						return commonfees.Compare(initialUnitFees, currUnitFees)
					}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
					tests.Outf("\n{{blue}}unit fees have decreased to %v{{/}}\n", currUnitFees)
				})
			},
		)
	})
})
