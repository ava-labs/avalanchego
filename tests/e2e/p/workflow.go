// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	xconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	xfees "github.com/ava-labs/avalanchego/vms/avm/txs/fees"
	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
	pconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
	psigner "github.com/ava-labs/avalanchego/vms/platformvm/signer"
	ptxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
	pfees "github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	xbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	ginkgo "github.com/onsi/ginkgo/v2"
)

// PChainWorkflow is an integration test for normal P-Chain operations
// - Issues an Add Validator and an Add Delegator using the funding address
// - Exports AVAX from the P-Chain funding address to the X-Chain created address
// - Exports AVAX from the X-Chain created address to the P-Chain created address
// - Checks the expected value of the funding address

var _ = e2e.DescribePChain("[Workflow]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("P-chain main operations",
		func() {
			nodeURI := e2e.Env.GetRandomNodeURI()
			keychain := e2e.Env.NewKeychain(2)
			baseWallet := e2e.NewWallet(keychain, nodeURI)

			pWallet := baseWallet.P()
			pBuilder := pWallet.Builder()
			pContext := pBuilder.Context()
			avaxAssetID := pContext.AVAXAssetID
			xWallet := baseWallet.X()
			xBuilder := xWallet.Builder()
			xContext := xBuilder.Context()
			pChainClient := platformvm.NewClient(nodeURI.URI)
			xChainClient := avm.NewClient(nodeURI.URI, "X")

			tests.Outf("{{blue}} fetching minimal stake amounts {{/}}\n")
			minValStake, minDelStake, err := pChainClient.GetMinStake(e2e.DefaultContext(), constants.PlatformChainID)
			require.NoError(err)
			tests.Outf("{{green}} minimal validator stake: %d {{/}}\n", minValStake)
			tests.Outf("{{green}} minimal delegator stake: %d {{/}}\n", minDelStake)

			tests.Outf("{{blue}} fetching X-chain tx fee {{/}}\n")
			infoClient := info.NewClient(nodeURI.URI)
			staticFees, err := infoClient.GetTxFee(e2e.DefaultContext())
			require.NoError(err)
			pChainStaticFees := &pconfig.Config{
				TxFee:                         uint64(staticFees.TxFee),
				CreateSubnetTxFee:             uint64(staticFees.CreateSubnetTxFee),
				TransformSubnetTxFee:          uint64(staticFees.TransformSubnetTxFee),
				CreateBlockchainTxFee:         uint64(staticFees.CreateBlockchainTxFee),
				AddPrimaryNetworkValidatorFee: uint64(staticFees.AddPrimaryNetworkValidatorFee),
				AddPrimaryNetworkDelegatorFee: uint64(staticFees.AddPrimaryNetworkDelegatorFee),
				AddSubnetValidatorFee:         uint64(staticFees.AddSubnetValidatorFee),
				AddSubnetDelegatorFee:         uint64(staticFees.AddSubnetDelegatorFee),
			}

			// amount to transfer from P to X chain
			toTransfer := 1 * units.Avax

			pShortAddr := keychain.Keys[0].Address()
			xTargetAddr := keychain.Keys[1].Address()

			// Use a random node ID to ensure that repeated test runs
			// will succeed against a network that persists across runs.
			validatorID, err := ids.ToNodeID(utils.RandomBytes(ids.NodeIDLen))
			require.NoError(err)

			vdr := &ptxs.SubnetValidator{
				Validator: ptxs.Validator{
					NodeID: validatorID,
					End:    uint64(time.Now().Add(72 * time.Hour).Unix()),
					Wght:   minValStake,
				},
				Subnet: constants.PrimaryNetworkID,
			}
			rewardOwner := &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{pShortAddr},
			}
			shares := uint32(20000) // TODO: retrieve programmatically

			sk, err := bls.NewSecretKey()
			require.NoError(err)
			pop := psigner.NewProofOfPossession(sk)

			ginkgo.By("issue add validator tx", func() {
				_, err := pWallet.IssueAddPermissionlessValidatorTx(
					vdr,
					pop,
					avaxAssetID,
					rewardOwner,
					rewardOwner,
					shares,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			ginkgo.By("issue add delegator tx", func() {
				_, err := pWallet.IssueAddPermissionlessDelegatorTx(
					vdr,
					avaxAssetID,
					rewardOwner,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)
			})

			// retrieve initial balances
			pBalances, err := pWallet.Builder().GetBalance()
			require.NoError(err)
			pStartBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance before P->X export: %d {{/}}\n", pStartBalance)

			xBalances, err := xWallet.Builder().GetFTBalance()
			require.NoError(err)
			xStartBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance before P->X export: %d {{/}}\n", xStartBalance)

			outputOwner := secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					xTargetAddr,
				},
			}
			output := &secp256k1fx.TransferOutput{
				Amt:          toTransfer,
				OutputOwners: outputOwner,
			}

			pChainExportFee := uint64(0)
			ginkgo.By("export avax from P to X chain", func() {
				_, nextFeeRates, err := pChainClient.GetFeeRates(e2e.DefaultContext())
				require.NoError(err)

				tx, err := pWallet.IssueExportTx(
					xContext.BlockchainID,
					[]*avax.TransferableOutput{
						{
							Asset: avax.Asset{
								ID: avaxAssetID,
							},
							Out: output,
						},
					},
					e2e.WithDefaultContext(),
				)
				require.NoError(err)

				// retrieve fees paid for the tx
				feeCfg := pconfig.GetDynamicFeesConfig(true /*isEActive*/)
				feeCalc := pfees.NewDynamicCalculator(pChainStaticFees, time.Time{}, commonfees.NewManager(nextFeeRates), feeCfg.BlockMaxComplexity, tx.Creds)
				require.NoError(tx.Unsigned.Visit(feeCalc))
				pChainExportFee = feeCalc.Fee
			})

			// check balances post export
			pBalances, err = pWallet.Builder().GetBalance()
			require.NoError(err)
			pPreImportBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance after P->X export: %d {{/}}\n", pPreImportBalance)

			xBalances, err = xWallet.Builder().GetFTBalance()
			require.NoError(err)
			xPreImportBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance after P->X export: %d {{/}}\n", xPreImportBalance)

			require.Equal(xPreImportBalance, xStartBalance) // import not performed yet
			require.Equal(pPreImportBalance, pStartBalance-toTransfer-pChainExportFee)

			xChainExportFee := uint64(0)
			ginkgo.By("import avax from P into X chain", func() {
				tx, err := xWallet.IssueImportTx(
					constants.PlatformChainID,
					&outputOwner,
					e2e.WithDefaultContext(),
				)
				require.NoError(err)

				// retrieve fees paid for the tx
				feeCfg := xconfig.GetDynamicFeesConfig(true /*isEActive*/)
				feeRates, _, err := xChainClient.GetFeeRates(e2e.DefaultContext())
				require.NoError(err)

				feeCalc := xfees.NewDynamicCalculator(
					xbuilder.Parser.Codec(),
					commonfees.NewManager(feeRates),
					feeCfg.BlockMaxComplexity,
					tx.Creds,
				)
				require.NoError(tx.Unsigned.Visit(feeCalc))
				xChainExportFee = feeCalc.Fee
			})

			// check balances post import
			pBalances, err = pWallet.Builder().GetBalance()
			require.NoError(err)
			pFinalBalance := pBalances[avaxAssetID]
			tests.Outf("{{blue}} P-chain balance after P->X import: %d {{/}}\n", pFinalBalance)

			xBalances, err = xWallet.Builder().GetFTBalance()
			require.NoError(err)
			xFinalBalance := xBalances[avaxAssetID]
			tests.Outf("{{blue}} X-chain balance after P->X import: %d {{/}}\n", xFinalBalance)

			require.Equal(xFinalBalance, xPreImportBalance+toTransfer-xChainExportFee) // import not performed yet
			require.Equal(pFinalBalance, pPreImportBalance)
		})
})
