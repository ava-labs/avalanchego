// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements X-Chain whitelist vtx (stop vertex) tests.
package whitelistvtx

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var keyFactory crypto.FactorySECP256K1R

var _ = e2e.DescribeXChain("[WhitelistTx]", func() {
	ginkgo.It("can issue whitelist vtx", func() {
		if !e2e.GetEnableWhitelistTxTests() {
			ginkgo.Skip("whitelist vtx tests are disabled; skipping")
		}

		uris := e2e.GetURIs()
		gomega.Expect(uris).ShouldNot(gomega.BeEmpty())

		randomKeyIntf, err := keyFactory.NewPrivateKey()
		gomega.Expect(err).Should(gomega.BeNil())

		randomKey := randomKeyIntf.(*crypto.PrivateKeySECP256K1R)
		randomAddr := randomKey.PublicKey().Address()
		keys := secp256k1fx.NewKeychain(
			genesis.EWOQKey,
			randomKey,
		)
		var wallet primary.Wallet
		ginkgo.By("collect whitelist vtx metrics", func() {
			walletURI := uris[0]

			// 5-second is enough to fetch initial UTXOs for test cluster in "primary.NewWallet"
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultWalletCreationTimeout)
			var err error
			wallet, err = primary.NewWalletFromURI(ctx, walletURI, keys)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())
		})

		allMetrics := []string{
			"avalanche_X_whitelist_vtx_issue_success",
			"avalanche_X_whitelist_vtx_issue_failure",
			"avalanche_X_whitelist_tx_processing",
			"avalanche_X_whitelist_tx_accepted_count",
			"avalanche_X_whitelist_tx_polls_accepted_count",
			"avalanche_X_whitelist_tx_rejected_count",
			"avalanche_X_whitelist_tx_polls_rejected_count",
		}

		// URI -> "metric name" -> "metric value"
		curMetrics := make(map[string]map[string]float64)
		ginkgo.By("collect whitelist vtx metrics", func() {
			for _, u := range uris {
				ep := u + "/ext/metrics"

				mm, err := tests.GetMetricsValue(ep, allMetrics...)
				gomega.Expect(err).Should(gomega.BeNil())
				tests.Outf("{{green}}metrics at %q:{{/}} %v\n", ep, mm)

				if mm["avalanche_X_whitelist_tx_accepted_count"] > 0 {
					tests.Outf("{{red}}{{bold}}%q already has whitelist vtx!!!{{/}}\n", u)
					ginkgo.Skip("the cluster has already accepted whitelist vtx thus skipping")
				}

				curMetrics[u] = mm
			}
		})

		ewoqWallet := primary.NewWalletWithOptions(
			wallet,
			common.WithCustomAddresses(ids.ShortSet{
				genesis.EWOQKey.PublicKey().Address(): struct{}{},
			}),
		)
		randWallet := primary.NewWalletWithOptions(
			wallet,
			common.WithCustomAddresses(ids.ShortSet{
				randomKey.PublicKey().Address(): struct{}{},
			}),
		)
		ginkgo.By("issue regular, virtuous X-Chain tx, before whitelist vtx, should succeed", func() {
			balances, err := ewoqWallet.X().Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())

			avaxAssetID := wallet.X().AVAXAssetID()
			ewoqPrevBalX := balances[avaxAssetID]
			tests.Outf("{{green}}ewoq wallet balance:{{/}} %d\n", ewoqPrevBalX)

			balances, err = randWallet.X().Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())

			randPrevBalX := balances[avaxAssetID]
			tests.Outf("{{green}}rand wallet balance:{{/}} %d\n", randPrevBalX)

			amount := genRandUint64(ewoqPrevBalX)
			tests.Outf("{{green}}amount to transfer:{{/}} %d\n", amount)

			tests.Outf("{{blue}}issuing regular, virtuous transaction at %q{{/}}\n", uris[0])
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
			_, err = ewoqWallet.X().IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: amount,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{randomAddr},
						},
					},
				}},
				common.WithContext(ctx),
			)
			cancel()
			gomega.Expect(err).Should(gomega.BeNil())

			time.Sleep(3 * time.Second)

			balances, err = ewoqWallet.X().Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			ewoqCurBalX := balances[avaxAssetID]
			tests.Outf("{{green}}ewoq wallet balance:{{/}} %d\n", ewoqCurBalX)

			balances, err = randWallet.X().Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())
			randCurBalX := balances[avaxAssetID]
			tests.Outf("{{green}}ewoq wallet balance:{{/}} %d\n", randCurBalX)

			gomega.Expect(ewoqCurBalX).Should(gomega.Equal(ewoqPrevBalX - amount - wallet.X().BaseTxFee()))
			gomega.Expect(randCurBalX).Should(gomega.Equal(randPrevBalX + amount))
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
				gomega.Expect(mm["avalanche_X_whitelist_vtx_issue_success"]).Should(gomega.Equal(prev["avalanche_X_whitelist_vtx_issue_success"] + 1))

				// +0 since no node ever failed to issue a whitelist vtx
				gomega.Expect(mm["avalanche_X_whitelist_vtx_issue_failure"]).Should(gomega.Equal(prev["avalanche_X_whitelist_vtx_issue_failure"]))

				// +0 since the local node snowstorm successfully issued the whitelist tx or received from the first node, and accepted
				gomega.Expect(mm["avalanche_X_whitelist_tx_processing"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_processing"]))

				// +1 since the local node snowstorm successfully accepted the whitelist tx or received from the first node
				gomega.Expect(mm["avalanche_X_whitelist_tx_accepted_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_accepted_count"] + 1))
				gomega.Expect(mm["avalanche_X_whitelist_tx_polls_accepted_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_polls_accepted_count"] + 1))

				// +0 since no node ever rejected a whitelist tx
				gomega.Expect(mm["avalanche_X_whitelist_tx_rejected_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_rejected_count"]))
				gomega.Expect(mm["avalanche_X_whitelist_tx_polls_rejected_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_polls_rejected_count"]))

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
				gomega.Expect(mm["avalanche_X_whitelist_vtx_issue_success"]).Should(gomega.Equal(prev["avalanche_X_whitelist_vtx_issue_success"]))

				// +1 since the local node engine failed the conflicting whitelist vtx issue request
				gomega.Expect(mm["avalanche_X_whitelist_vtx_issue_failure"]).Should(gomega.Equal(prev["avalanche_X_whitelist_vtx_issue_failure"] + 1))

				// +0 since the local node snowstorm successfully issued the whitelist tx "before", and no whitelist tx is being processed
				gomega.Expect(mm["avalanche_X_whitelist_tx_processing"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_processing"]))

				// +0 since the local node snowstorm successfully accepted the whitelist tx "before"
				gomega.Expect(mm["avalanche_X_whitelist_tx_accepted_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_accepted_count"]))
				gomega.Expect(mm["avalanche_X_whitelist_tx_polls_accepted_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_polls_accepted_count"]))

				// +0 since the local node snowstorm never rejected a whitelist tx
				gomega.Expect(mm["avalanche_X_whitelist_tx_rejected_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_rejected_count"]))
				gomega.Expect(mm["avalanche_X_whitelist_tx_polls_rejected_count"]).Should(gomega.Equal(prev["avalanche_X_whitelist_tx_polls_rejected_count"]))

				curMetrics[u] = mm
			}
		})

		ginkgo.By("issue regular, virtuous X-Chain tx, after whitelist vtx, should fail", func() {
			balances, err := ewoqWallet.X().Builder().GetFTBalance()
			gomega.Expect(err).Should(gomega.BeNil())

			avaxAssetID := wallet.X().AVAXAssetID()
			ewoqPrevBalX := balances[avaxAssetID]
			tests.Outf("{{green}}ewoq wallet balance:{{/}} %d\n", ewoqPrevBalX)

			amount := genRandUint64(ewoqPrevBalX)
			tests.Outf("{{blue}}issuing regular, virtuous transaction at %q{{/}}\n", uris[0])
			ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultConfirmTxTimeout)
			_, err = ewoqWallet.X().IssueBaseTx(
				[]*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: amount,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{randomAddr},
						},
					},
				}},
				common.WithContext(ctx),
			)
			cancel()
			gomega.Expect(err.Error()).Should(gomega.ContainSubstring(context.DeadlineExceeded.Error()))

			ep := uris[0] + "/ext/metrics"
			mm, err := tests.GetMetricsValue(ep, allMetrics...)
			gomega.Expect(err).Should(gomega.BeNil())

			// regular, virtuous transaction should not change whitelist vtx metrics
			prev := curMetrics[uris[0]]
			gomega.Expect(mm).Should(gomega.Equal(prev))
			curMetrics[uris[0]] = mm
		})
	})
})

// use lower 5% as upper-bound
// we don't want to transfer all at once
// which fails all subsequent requests.
func genRandUint64(max uint64) uint64 {
	mb := new(big.Int).SetUint64(max / 20)
	nBig, err := rand.Int(rand.Reader, mb)
	if err != nil {
		return 0
	}
	return nBig.Uint64()
}
