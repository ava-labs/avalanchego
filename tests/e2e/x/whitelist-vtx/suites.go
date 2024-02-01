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

// Implements X-Chain whitelist vtx (stop vertex) tests.
package whitelistvtx

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	"github.com/chain4travel/camino-node/tests"
	"github.com/chain4travel/camino-node/tests/e2e"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	metricVtxIssueSuccess = "camino_X_avalanche_whitelist_vtx_issue_success"
	metricVtxIssueFailure = "camino_X_avalanche_whitelist_vtx_issue_failure"
	metricTxProcessing    = "camino_X_avalanche_whitelist_tx_processing"
	metricTxAccepted      = "camino_X_avalanche_whitelist_tx_accepted_count"
	metricTxRejected      = "camino_X_avalanche_whitelist_tx_rejected_count"
	metricTxPollsAccepted = "camino_X_avalanche_whitelist_tx_polls_accepted_count"
	metricTxPollsRejected = "camino_X_avalanche_whitelist_tx_polls_rejected_count"
)

var _ = e2e.DescribeXChain("[WhitelistTx]", func() {
	ginkgo.It("can issue whitelist vtx",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"require-network-runner",
			"x",
			"whitelist-tx",
		),
		func() {
			uris := e2e.Env.GetURIs()
			gomega.Expect(uris).ShouldNot(gomega.BeEmpty())

			testKeys, testKeyAddrs, keyChain := e2e.Env.GetTestKeys()
			var baseWallet primary.Wallet
			ginkgo.By("collect whitelist vtx metrics", func() {
				walletURI := uris[0]

				// 5-second is enough to fetch initial UTXOs for test cluster in "primary.NewWallet"
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
				var err error
				baseWallet, err = primary.NewWalletFromURI(ctx, walletURI, keyChain)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				if baseWallet.P().NetworkID() == constants.MainnetID {
					ginkgo.Skip("skipping tests (mainnet)")
				}
			})
			avaxAssetID := baseWallet.X().AVAXAssetID()
			wallets := make([]primary.Wallet, len(testKeys))
			for i := range wallets {
				wallets[i] = primary.NewWalletWithOptions(
					baseWallet,
					common.WithCustomAddresses(set.Set[ids.ShortID]{
						testKeys[i].PublicKey().Address(): struct{}{},
					}),
				)
			}

			allMetrics := []string{
				metricVtxIssueSuccess,
				metricVtxIssueFailure,
				metricTxProcessing,
				metricTxAccepted,
				metricTxRejected,
				metricTxPollsAccepted,
				metricTxPollsRejected,
			}

			// URI -> "metric name" -> "metric value"
			curMetrics := make(map[string]map[string]float64)
			ginkgo.By("collect whitelist vtx metrics", func() {
				for _, u := range uris {
					ep := u + "/ext/metrics"

					mm, err := tests.GetMetricsValue(ep, allMetrics...)
					gomega.Expect(err).Should(gomega.BeNil())
					tests.Outf("{{green}}metrics at %q:{{/}} %v\n", ep, mm)

					if mm[metricTxAccepted] > 0 {
						tests.Outf("{{red}}{{bold}}%q already has whitelist vtx!!!{{/}}\n", u)
						ginkgo.Skip("the cluster has already accepted whitelist vtx thus skipping")
					}

					curMetrics[u] = mm
				}
			})

			ginkgo.By("issue regular, virtuous X-Chain tx, before whitelist vtx, should succeed", func() {
				balances, err := wallets[0].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())
				key1PrevBalX := balances[avaxAssetID]
				tests.Outf("{{green}}first wallet balance:{{/}} %d\n", key1PrevBalX)

				balances, err = wallets[1].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())

				key2PrevBalX := balances[avaxAssetID]
				tests.Outf("{{green}}second wallet balance:{{/}} %d\n", key2PrevBalX)

				transferAmount := key1PrevBalX / 10
				gomega.Expect(transferAmount).Should(gomega.BeNumerically(">", 0.0), "not enough balance in the test wallet")
				tests.Outf("{{green}}amount to transfer:{{/}} %d\n", transferAmount)

				tests.Outf("{{blue}}issuing regular, virtuous transaction at %q{{/}}\n", uris[0])
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err = wallets[0].X().IssueBaseTx(
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: transferAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{testKeyAddrs[1]},
							},
						},
					}},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				time.Sleep(3 * time.Second)

				balances, err = wallets[0].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())
				key1CurBalX := balances[avaxAssetID]
				tests.Outf("{{green}}first wallet balance:{{/}} %d\n", key1CurBalX)

				balances, err = wallets[1].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())
				key2CurBalX := balances[avaxAssetID]
				tests.Outf("{{green}}second wallet balance:{{/}} %d\n", key2CurBalX)

				gomega.Expect(key1CurBalX).Should(gomega.Equal(key1PrevBalX - transferAmount - baseWallet.X().BaseTxFee()))
				gomega.Expect(key2CurBalX).Should(gomega.Equal(key2PrevBalX + transferAmount))
			})

			// issue a whitelist vtx to the first node
			// to trigger "Notify(common.StopVertex)", "t.issueStopVtx()", and "handleAsyncMsg"
			// this is the very first whitelist vtx issue request
			// SO THIS SHOULD SUCCEED WITH NO ERROR
			ginkgo.By("issue whitelist vtx to the first node", func() {
				tests.Outf("{{blue}}{{bold}}issuing whitelist vtx at URI %q at the very first time{{/}}\n", uris[0])
				client := avm.NewClient(uris[0], "X")
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				err := client.IssueStopVertex(ctx)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
				tests.Outf("{{blue}}issued whitelist vtx at %q{{/}}\n", uris[0])
			})

			ginkgo.By("accept the whitelist vtx in all nodes", func() {
				tests.Outf("{{blue}}waiting before checking the status of whitelist vtx{{/}}\n")
				time.Sleep(5 * time.Second) // should NOT take too long for all nodes to accept whitelist vtx

				for _, u := range uris {
					ep := u + "/ext/metrics"
					mm, err := tests.GetMetricsValue(ep, allMetrics...)
					gomega.Expect(err).Should(gomega.BeNil())

					prev := curMetrics[u]

					// +1 since the local node engine issues a new whitelist vtx
					gomega.Expect(mm[metricVtxIssueSuccess]).Should(gomega.Equal(prev[metricVtxIssueSuccess] + 1))

					// +0 since no node ever failed to issue a whitelist vtx
					gomega.Expect(mm[metricVtxIssueFailure]).Should(gomega.Equal(prev[metricVtxIssueFailure]))

					// +0 since the local node snowstorm successfully issued the whitelist tx or received from the first node, and accepted
					gomega.Expect(mm[metricTxProcessing]).Should(gomega.Equal(prev[metricTxProcessing]))

					// +1 since the local node snowstorm successfully accepted the whitelist tx or received from the first node
					gomega.Expect(mm[metricTxAccepted]).Should(gomega.Equal(prev[metricTxAccepted] + 1))
					gomega.Expect(mm[metricTxPollsAccepted]).Should(gomega.Equal(prev[metricTxPollsAccepted] + 1))

					// +0 since no node ever rejected a whitelist tx
					gomega.Expect(mm[metricTxRejected]).Should(gomega.Equal(prev[metricTxRejected]))
					gomega.Expect(mm[metricTxPollsRejected]).Should(gomega.Equal(prev[metricTxPollsRejected]))

					curMetrics[u] = mm
				}
			})

			// to trigger "Notify(common.StopVertex)" and "t.issueStopVtx()", or "Put"
			// this is the second, conflicting whitelist vtx issue request
			// SO THIS MUST FAIL WITH ERROR IN ALL NODES
			ginkgo.By("whitelist vtx can't be issued twice in all nodes", func() {
				for _, u := range uris {
					tests.Outf("{{red}}issuing second whitelist vtx to URI %q{{/}}\n", u)
					client := avm.NewClient(u, "X")
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					err := client.IssueStopVertex(ctx)
					cancel()
					gomega.Expect(err).Should(gomega.BeNil()) // issue itself is asynchronous, so the internal error is not exposed!

					// the local node should see updates on the metrics
					time.Sleep(3 * time.Second)

					ep := u + "/ext/metrics"
					mm, err := tests.GetMetricsValue(ep, allMetrics...)
					gomega.Expect(err).Should(gomega.BeNil())

					prev := curMetrics[u]

					// +0 since no node should ever successfully issue another whitelist vtx
					gomega.Expect(mm[metricVtxIssueSuccess]).Should(gomega.Equal(prev[metricVtxIssueSuccess]))

					// +0 since the local node engine should have dropped the conflicting whitelist vtx issue request
					gomega.Expect(mm[metricVtxIssueFailure]).Should(gomega.Equal(prev[metricVtxIssueFailure]))

					// +0 since the local node snowstorm successfully issued the whitelist tx "before", and no whitelist tx is being processed
					gomega.Expect(mm[metricTxProcessing]).Should(gomega.Equal(prev[metricTxProcessing]))

					// +0 since the local node snowstorm successfully accepted the whitelist tx "before"
					gomega.Expect(mm[metricTxAccepted]).Should(gomega.Equal(prev[metricTxAccepted]))
					gomega.Expect(mm[metricTxPollsAccepted]).Should(gomega.Equal(prev[metricTxPollsAccepted]))

					// +0 since the local node snowstorm never rejected a whitelist tx
					gomega.Expect(mm[metricTxRejected]).Should(gomega.Equal(prev[metricTxRejected]))
					gomega.Expect(mm[metricTxPollsRejected]).Should(gomega.Equal(prev[metricTxPollsRejected]))

					curMetrics[u] = mm
				}
			})

			ginkgo.By("issue regular, virtuous X-Chain tx, after whitelist vtx, should pass", func() {
				balances, err := wallets[0].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())

				avaxAssetID := baseWallet.X().AVAXAssetID()
				key1PrevBalX := balances[avaxAssetID]
				tests.Outf("{{green}}first wallet balance:{{/}} %d\n", key1PrevBalX)

				transferAmount := key1PrevBalX / 10
				gomega.Expect(transferAmount).Should(gomega.BeNumerically(">", 0.0), "not enough balance in the test wallet")
				tests.Outf("{{blue}}issuing regular, virtuous transaction at %q{{/}}\n", uris[0])
				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				_, err = wallets[0].X().IssueBaseTx(
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: transferAmount,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{testKeyAddrs[1]},
							},
						},
					}},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())
			})
		})
})
