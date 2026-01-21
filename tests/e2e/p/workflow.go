// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// PChainWorkflow is an integration test for normal P-Chain operations
// - Issues an AddPermissionlessValidatorTx
// - Issues an AddPermissionlessDelegatorTx
// - Issues an ExportTx on the P-chain and verifies the expected balances
// - Issues an ImportTx on the X-chain and verifies the expected balances

var _ = e2e.DescribePChain("[Workflow]", func() {
	var (
		tc      = e2e.NewTestContext()
		require = require.New(tc)
	)

	ginkgo.It("P-chain main operations", func() {
		const (
			// amount to transfer from P to X chain
			toTransfer                 = 1 * units.Avax
			delegationFeeShares uint32 = 20000 // TODO: retrieve programmatically
		)

		env := e2e.GetEnv(tc)

		// Use a pre-funded key for the P-Chain
		keychain := env.NewKeychain()
		// Use a new key for the X-Chain
		keychain.Add(e2e.NewPrivateKey(tc))

		var (
			nodeURI = env.GetRandomNodeURI()

			rewardAddr  = keychain.Keys[0].Address()
			rewardOwner = &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{rewardAddr},
			}

			transferAddr  = keychain.Keys[1].Address()
			transferOwner = secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{transferAddr},
			}

			// Ensure the change is returned to the pre-funded key
			// TODO(marun) Remove when the wallet does this automatically
			changeOwner = common.WithChangeOwner(&secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					keychain.Keys[0].Address(),
				},
			})

			baseWallet = e2e.NewWallet(tc, keychain, nodeURI)

			pWallet        = baseWallet.P()
			pBuilder       = pWallet.Builder()
			pContext       = pBuilder.Context()
			pFeeCalculator = e2e.NewPChainFeeCalculatorFromContext(pContext)

			xWallet  = baseWallet.X()
			xBuilder = xWallet.Builder()
			xContext = xBuilder.Context()

			avaxAssetID = pContext.AVAXAssetID
		)

		tc.Log().Info("fetching minimal stake amounts")
		pChainClient := platformvm.NewClient(nodeURI.URI)
		minValStake, minDelStake, err := pChainClient.GetMinStake(
			tc.DefaultContext(),
			constants.PlatformChainID,
		)
		require.NoError(err)
		tc.Log().Info("fetched minimal stake amounts",
			zap.Uint64("minValidatorStake", minValStake),
			zap.Uint64("minDelegatorStake", minDelStake),
		)

		// Use a random node ID to ensure that repeated test runs will succeed
		// against a network that persists across runs.
		validatorID, err := ids.ToNodeID(utils.RandomBytes(ids.NodeIDLen))
		require.NoError(err)

		vdr := &txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: validatorID,
				End:    uint64(time.Now().Add(72 * time.Hour).Unix()),
				Wght:   minValStake,
			},
			Subnet: constants.PrimaryNetworkID,
		}

		tc.By("issuing an AddPermissionlessValidatorTx", func() {
			sk, err := localsigner.New()
			require.NoError(err)
			pop, err := signer.NewProofOfPossession(sk)
			require.NoError(err)

			_, err = pWallet.IssueAddPermissionlessValidatorTx(
				vdr,
				pop,
				avaxAssetID,
				rewardOwner,
				rewardOwner,
				delegationFeeShares,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("issuing an AddPermissionlessDelegatorTx", func() {
			_, err := pWallet.IssueAddPermissionlessDelegatorTx(
				vdr,
				avaxAssetID,
				rewardOwner,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)
		})

		tc.By("issuing an ExportTx on the P-chain", func() {
			balances, err := pBuilder.GetBalance()
			require.NoError(err)

			initialAVAXBalance := balances[avaxAssetID]
			tc.Log().Info("retrieved P-chain balance before P->X export",
				zap.Uint64("balance", initialAVAXBalance),
			)

			exportTx, err := pWallet.IssueExportTx(
				xContext.BlockchainID,
				[]*avax.TransferableOutput{
					{
						Asset: avax.Asset{
							ID: avaxAssetID,
						},
						Out: &secp256k1fx.TransferOutput{
							Amt:          toTransfer,
							OutputOwners: transferOwner,
						},
					},
				},
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)

			exportFee, err := pFeeCalculator.CalculateFee(exportTx.Unsigned)
			require.NoError(err)

			balances, err = pBuilder.GetBalance()
			require.NoError(err)

			finalAVAXBalance := balances[avaxAssetID]
			tc.Log().Info("retrieved P-chain balance after P->X export",
				zap.Uint64("balance", finalAVAXBalance),
			)

			require.Equal(initialAVAXBalance-toTransfer-exportFee, finalAVAXBalance)
		})

		tc.By("issuing an ImportTx on the X-Chain", func() {
			balances, err := xBuilder.GetFTBalance()
			require.NoError(err)

			initialAVAXBalance := balances[avaxAssetID]
			tc.Log().Info("retrieved X-chain balance before P->X import",
				zap.Uint64("balance", initialAVAXBalance),
			)

			_, err = xWallet.IssueImportTx(
				constants.PlatformChainID,
				&transferOwner,
				tc.WithDefaultContext(),
				changeOwner,
			)
			require.NoError(err)

			balances, err = xBuilder.GetFTBalance()
			require.NoError(err)

			finalAVAXBalance := balances[avaxAssetID]
			tc.Log().Info("retrieved X-chain balance after P->X import",
				zap.Uint64("balance", finalAVAXBalance),
			)

			require.Equal(initialAVAXBalance+toTransfer-xContext.BaseTxFee, finalAVAXBalance)
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
