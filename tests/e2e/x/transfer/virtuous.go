// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements X-chain transfer tests.
package transfer

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	metricVtxProcessing = "avalanche_X_avalanche_vtx_processing"
	metricVtxAccepted   = "avalanche_X_avalanche_vtx_accepted_count"
	metricVtxRejected   = "avalanche_X_avalanche_vtx_rejected_count"
)

const totalRounds = 50

var _ = e2e.DescribeXChain("[Virtuous Transfer Tx AVAX]", func() {
	ginkgo.It("can issue a virtuous transfer tx for AVAX asset",
		// use this for filtering tests by labels
		// ref. https://onsi.github.io/ginkgo/#spec-labels
		ginkgo.Label(
			"require-network-runner",
			"x",
			"virtuous-transfer-tx-avax",
		),
		func() {
			rpcEps := e2e.Env.GetURIs()
			gomega.Expect(rpcEps).ShouldNot(gomega.BeEmpty())

			allMetrics := []string{
				metricVtxProcessing,
				metricVtxAccepted,
				metricVtxRejected,
			}

			runFunc := func(round int) {
				tests.Outf("{{green}}\n\n\n\n\n\n---\n[ROUND #%02d]:{{/}}\n", round)

				testKeys, _, _ := e2e.Env.GetTestKeys()

				needPermute := round > 3
				if needPermute {
					rand.Seed(time.Now().UnixNano())
					rand.Shuffle(len(testKeys), func(i, j int) {
						testKeys[i], testKeys[j] = testKeys[j], testKeys[i]
					})
				}
				keyChain := secp256k1fx.NewKeychain(testKeys...)

				var baseWallet primary.Wallet
				var err error
				ginkgo.By("setting up a base wallet", func() {
					walletURI := rpcEps[0]

					// 5-second is enough to fetch initial UTXOs for test cluster in "primary.NewWallet"
					ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
					baseWallet, err = primary.NewWalletFromURI(ctx, walletURI, keyChain)
					cancel()
					gomega.Expect(err).Should(gomega.BeNil())
				})
				avaxAssetID := baseWallet.X().AVAXAssetID()

				wallets := make([]primary.Wallet, len(testKeys))
				shortAddrs := make([]ids.ShortID, len(testKeys))
				for i := range wallets {
					shortAddrs[i] = testKeys[i].PublicKey().Address()

					wallets[i] = primary.NewWalletWithOptions(
						baseWallet,
						common.WithCustomAddresses(set.Set[ids.ShortID]{
							testKeys[i].PublicKey().Address(): struct{}{},
						}),
					)
				}

				// URI -> "metric name" -> "metric value"
				metricsBeforeTx := make(map[string]map[string]float64)
				for _, u := range rpcEps {
					ep := u + "/ext/metrics"

					mm, err := tests.GetMetricsValue(ep, allMetrics...)
					gomega.Expect(err).Should(gomega.BeNil())
					tests.Outf("{{green}}metrics at %q:{{/}} %v\n", ep, mm)

					if mm[metricVtxProcessing] > 0 {
						tests.Outf("{{red}}{{bold}}%q already has processing vtx!!!{{/}}\n", u)
						ginkgo.Skip("the cluster has already ongoing vtx txs thus skipping to prevent conflicts...")
					}

					metricsBeforeTx[u] = mm
				}

				testBalances := make([]uint64, 0)
				for i, w := range wallets {
					balances, err := w.X().Builder().GetFTBalance()
					gomega.Expect(err).Should(gomega.BeNil())

					bal := balances[avaxAssetID]
					testBalances = append(testBalances, bal)

					fmt.Printf(`CURRENT BALANCE %21d AVAX (SHORT ADDRESS %q)
`,
						bal,
						testKeys[i].PublicKey().Address(),
					)
				}
				fromIdx := -1
				for i := range testBalances {
					if fromIdx < 0 && testBalances[i] > 0 {
						fromIdx = i
						break
					}
				}
				if fromIdx < 0 {
					gomega.Expect(fromIdx).Should(gomega.BeNumerically(">", 0), "no address found with non-zero balance")
				}

				toIdx := -1
				for i := range testBalances {
					// prioritize the address with zero balance
					if toIdx < 0 && i != fromIdx && testBalances[i] == 0 {
						toIdx = i
						break
					}
				}
				if toIdx < 0 {
					// no zero balance address, so just transfer between any two addresses
					toIdx = (fromIdx + 1) % len(testBalances)
				}

				senderOrigBal := testBalances[fromIdx]
				receiverOrigBal := testBalances[toIdx]

				amountToTransfer := senderOrigBal / 10

				senderNewBal := senderOrigBal - amountToTransfer - baseWallet.X().BaseTxFee()
				receiverNewBal := receiverOrigBal + amountToTransfer

				ginkgo.By("X-Chain transfer with wrong amount must fail", func() {
					ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
					_, err = wallets[fromIdx].X().IssueBaseTx(
						[]*avax.TransferableOutput{{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: &secp256k1fx.TransferOutput{
								Amt: senderOrigBal + 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{shortAddrs[toIdx]},
								},
							},
						}},
						common.WithContext(ctx),
					)
					cancel()
					gomega.Expect(err.Error()).Should(gomega.ContainSubstring("insufficient funds"))
				})

				fmt.Printf(`===
TRANSFERRING

FROM [%q]
SENDER    CURRENT BALANCE     : %21d AVAX
SENDER    NEW BALANCE (AFTER) : %21d AVAX

TRANSFER AMOUNT FROM SENDER   : %21d AVAX

TO [%q]
RECEIVER  CURRENT BALANCE     : %21d AVAX
RECEIVER  NEW BALANCE (AFTER) : %21d AVAX
===
`,
					shortAddrs[fromIdx],
					senderOrigBal,
					senderNewBal,
					amountToTransfer,
					shortAddrs[toIdx],
					receiverOrigBal,
					receiverNewBal,
				)

				ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
				txID, err := wallets[fromIdx].X().IssueBaseTx(
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt: amountToTransfer,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{shortAddrs[toIdx]},
							},
						},
					}},
					common.WithContext(ctx),
				)
				cancel()
				gomega.Expect(err).Should(gomega.BeNil())

				balances, err := wallets[fromIdx].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())
				senderCurBalX := balances[avaxAssetID]
				tests.Outf("{{green}}first wallet balance:{{/}}  %d\n", senderCurBalX)

				balances, err = wallets[toIdx].X().Builder().GetFTBalance()
				gomega.Expect(err).Should(gomega.BeNil())
				receiverCurBalX := balances[avaxAssetID]
				tests.Outf("{{green}}second wallet balance:{{/}} %d\n", receiverCurBalX)

				gomega.Expect(senderCurBalX).Should(gomega.Equal(senderNewBal))
				gomega.Expect(receiverCurBalX).Should(gomega.Equal(receiverNewBal))

				for _, u := range rpcEps {
					xc := avm.NewClient(u, "X")
					ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
					status, err := xc.ConfirmTx(ctx, txID, 2*time.Second)
					cancel()
					gomega.Expect(err).Should(gomega.BeNil())
					gomega.Expect(status).Should(gomega.Equal(choices.Accepted))
				}

				for _, u := range rpcEps {
					xc := avm.NewClient(u, "X")
					ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
					status, err := xc.ConfirmTx(ctx, txID, 2*time.Second)
					cancel()
					gomega.Expect(err).Should(gomega.BeNil())
					gomega.Expect(status).Should(gomega.Equal(choices.Accepted))

					ep := u + "/ext/metrics"
					mm, err := tests.GetMetricsValue(ep, allMetrics...)
					gomega.Expect(err).Should(gomega.BeNil())

					prev := metricsBeforeTx[u]

					// +0 since X-chain tx must have been processed and accepted by now
					gomega.Expect(mm[metricVtxProcessing]).Should(gomega.Equal(prev[metricVtxProcessing]))

					// +1 since X-chain tx must have been accepted by now
					gomega.Expect(mm[metricVtxAccepted]).Should(gomega.Equal(prev[metricVtxAccepted] + 1))

					// +0 since virtuous X-chain tx must not be rejected
					gomega.Expect(mm[metricVtxRejected]).Should(gomega.Equal(prev[metricVtxRejected]))

					metricsBeforeTx[u] = mm
				}
			}

			for i := 0; i < totalRounds; i++ {
				runFunc(i)
				time.Sleep(time.Second)
			}
		})
})
