// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/client"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/crosschain"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethparams "github.com/ava-labs/libevm/params"
)

var _ = e2e.DescribeCChain("[EVM Wallet Transfers]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const txAmount = 1 * units.Avax

	ginkgo.It("should export to and import from the P-Chain with ordinary EVM transactions", func() {
		env := e2e.GetEnv(tc)

		nodeURI := env.GetRandomNodeURI()
		upgrades, err := info.NewClient(nodeURI.URI).Upgrades(tc.DefaultContext())
		require.NoError(err)
		if !upgrades.IsHeliconActivated(time.Now()) {
			ginkgo.Skip("skipping test because helicon isn't active")
		}

		tc.By("initializing a new eth client")
		ethClient := e2e.NewEthClient(tc, nodeURI)

		var (
			key        = env.PreFundedKey
			ethAddress = key.EthAddress()
			pAddress   = key.Address()
		)
		cChainID, err := ethClient.ChainID(tc.DefaultContext())
		require.NoError(err)
		signer := types.LatestSignerForChainID(cChainID)
		precompile := crosschain.ContractAddress

		sendToPrecompile := func(data []byte, value *big.Int) *types.Receipt {
			nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
			require.NoError(err)
			gasPrice := e2e.SuggestGasPrice(tc, ethClient)
			signedTx, err := types.SignTx(types.NewTx(&types.LegacyTx{
				Nonce: nonce, To: &precompile, Value: value, Gas: 200_000, GasPrice: gasPrice, Data: data,
			}), signer, key.ToECDSA())
			require.NoError(err)
			return e2e.SendEthTransaction(tc, ethClient, signedTx)
		}

		tc.By("exporting AVAX from the C-Chain to the P-Chain with one precompile call", func() {
			data, err := crosschain.ABI.Pack("exportAVAX", [32]byte(constants.PlatformChainID), common.Address(pAddress))
			require.NoError(err)
			receipt := sendToPrecompile(data, new(big.Int).SetUint64(txAmount*ethparams.GWei/units.NanoAvax))
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
			exports, _, err := crosschain.FromReceipts(types.Receipts{receipt})
			require.NoError(err)
			require.Len(exports, 1)
			require.Equal(pAddress, exports[0].To)
		})

		tc.By("initializing a keychain and associated wallet")
		keychain := secp256k1fx.NewKeychain(key)
		baseWallet := e2e.NewWallet(tc, keychain, nodeURI)
		pWallet := baseWallet.P()
		cContext := baseWallet.C().Builder().Context()
		avaxAssetID := pWallet.Builder().Context().AVAXAssetID
		owner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{pAddress}}

		tc.By("importing the exported AVAX on the P-Chain, as today", func() {
			// The UTXO reaches shared memory after the C-Chain block executes.
			tc.Eventually(func() bool {
				_, err := pWallet.IssueImportTx(cContext.BlockchainID, &owner, tc.WithDefaultContext())
				return err == nil
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to import on the P-Chain before timeout")
		})

		tc.By("exporting AVAX from the P-Chain to the EVM address, as today", func() {
			_, err := pWallet.IssueExportTx(cContext.BlockchainID, []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          txAmount,
					OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(ethAddress)}},
				},
			}}, tc.WithDefaultContext())
			require.NoError(err)
		})

		var waiting []*avax.UTXO
		tc.By("finding the UTXO in shared memory", func() {
			atomicClient := client.NewClient(nodeURI.URI, "C")
			tc.Eventually(func() bool {
				utxoBytes, _, _, err := atomicClient.GetAtomicUTXOs(tc.DefaultContext(), []ids.ShortID{ids.ShortID(ethAddress)}, "P", 1024, ids.ShortEmpty, ids.Empty)
				require.NoError(err)
				waiting = waiting[:0]
				for _, b := range utxoBytes {
					utxo, err := tx.ParseUTXO(b)
					require.NoError(err)
					waiting = append(waiting, utxo)
				}
				return len(waiting) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see the UTXO in shared memory before timeout")
		})

		tc.By("importing the UTXO on the C-Chain with one precompile call", func() {
			before, err := ethClient.BalanceAt(tc.DefaultContext(), ethAddress, nil)
			require.NoError(err)
			utxoIDs := make([]crosschain.UTXOID, len(waiting))
			var total uint64
			for i, u := range waiting {
				utxoIDs[i] = crosschain.UTXOID{TxID: u.TxID, OutputIndex: u.OutputIndex}
				total += u.Out.(*secp256k1fx.TransferOutput).Amt
			}
			data, err := crosschain.ABI.Pack("importUTXOs", utxoIDs, ethAddress)
			require.NoError(err)
			receipt := sendToPrecompile(data, nil)
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
			_, imports, err := crosschain.FromReceipts(types.Receipts{receipt})
			require.NoError(err)
			require.Len(imports, len(waiting))

			after, err := ethClient.BalanceAt(tc.DefaultContext(), ethAddress, nil)
			require.NoError(err)
			gas := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
			credited := new(big.Int).Sub(after, before)
			credited.Add(credited, gas)
			require.Equal(new(big.Int).Mul(new(big.Int).SetUint64(total), big.NewInt(ethparams.GWei)), credited)
		})

		_ = e2e.CheckBootstrapIsPossible(tc, env.GetNetwork())
	})
})
