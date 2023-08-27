// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"errors"
	"math/big"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

// This test uses the compiled bin for `hashing.sol` as
// well as its ABI contained in `hashing_contract.go`.

// This test needs to be run serially to minimize the potential for
// other tests to affect the gas price
var _ = e2e.DescribeCChainSerial("[Dynamic Fees]", func() {
	require := require.New(ginkgo.GinkgoT())

	// Need a gas limit much larger than the standard 21_000 to enable
	// the contract to induce a gas price increase
	const largeGasLimit = uint64(8_000_000)

	// TODO(marun) What is the significance of this value?
	gasTip := big.NewInt(1000 * params.GWei)

	ginkgo.It("should ensure that the gas price is affected by load", func() {
		ginkgo.By("allocating a pre-funded key")
		key := e2e.Env.AllocateFundedKey()
		ethAddress := evm.GetEthAddress(key)

		ginkgo.By("initializing a coreth client")
		ethClient := e2e.Env.NewEthClient(e2e.Env.GetRandomNodeURI())

		ginkgo.By("initializing a transaction signer")
		cChainID, err := ethClient.ChainID(e2e.DefaultContext())
		require.NoError(err)
		signer := types.NewEIP155Signer(cChainID)
		ecdsaKey := key.ToECDSA()
		sign := func(tx *types.Transaction) *types.Transaction {
			signedTx, err := types.SignTx(tx, signer, ecdsaKey)
			require.NoError(err)
			return signedTx
		}

		var contractAddress common.Address
		ginkgo.By("deploying an expensive contract", func() {
			// Create transaction
			nonce, err := ethClient.AcceptedNonceAt(e2e.DefaultContext(), ethAddress)
			require.NoError(err)
			compiledContract := common.Hex2Bytes(hashingCompiledContract)
			tx := types.NewTx(&types.LegacyTx{
				Nonce:    nonce,
				GasPrice: gasTip,
				Gas:      largeGasLimit,
				Value:    common.Big0,
				Data:     compiledContract,
			})

			// Send the transaction and wait for acceptance
			signedTx := sign(tx)
			require.NoError(ethClient.SendTransaction(e2e.DefaultContext(), signedTx))
			receipt := awaitTransaction(ethClient, signedTx)

			contractAddress = receipt.ContractAddress
		})

		var gasPrice *big.Int
		ginkgo.By("calling the expensive contract repeatedly until a gas price increase is detected", func() {
			// Evaluate the bytes representation of the contract
			hashingABI, err := abi.JSON(strings.NewReader(hashingABIJson))
			require.NoError(err)
			contractData, err := hashingABI.Pack("hashIt")
			require.NoError(err)

			var initialGasPrice *big.Int
			e2e.Eventually(func() bool {
				// Check the gas price
				var err error
				gasPrice, err = ethClient.SuggestGasPrice(e2e.DefaultContext())
				require.NoError(err)
				if initialGasPrice == nil {
					initialGasPrice = gasPrice
					tests.Outf("{{blue}}initial gas price is %v{{/}}\n", initialGasPrice)
				} else if gasPrice.Cmp(initialGasPrice) > 0 {
					// Gas price has increased
					tests.Outf("{{blue}}gas price has increased to %v{{/}}\n", gasPrice)
					return true
				}

				// Create the transaction
				nonce, err := ethClient.AcceptedNonceAt(e2e.DefaultContext(), ethAddress)
				require.NoError(err)
				tx := types.NewTx(&types.LegacyTx{
					Nonce:    nonce,
					GasPrice: gasTip,
					Gas:      largeGasLimit,
					To:       &contractAddress,
					Value:    common.Big0,
					Data:     contractData,
				})

				// Send the transaction and wait for acceptance
				signedTx := sign(tx)
				require.NoError(ethClient.SendTransaction(e2e.DefaultContext(), signedTx))
				_ = awaitTransaction(ethClient, signedTx)

				// The gas price will be checked at the start of the next iteration
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price increase before timeout")
		})

		ginkgo.By("waiting for the gas price to decrease...", func() {
			initialGasPrice := gasPrice
			e2e.Eventually(func() bool {
				var err error
				gasPrice, err = ethClient.SuggestGasPrice(e2e.DefaultContext())
				require.NoError(err)
				tests.Outf("{{blue}}.{{/}}")
				return initialGasPrice.Cmp(gasPrice) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
			tests.Outf("\n{{blue}}gas price has decreased to %v{{/}}\n", gasPrice)
		})

		ginkgo.By("sending funds at the current gas price", func() {
			// Create a recipient address
			factory := secp256k1.Factory{}
			recipientKey, err := factory.NewPrivateKey()
			require.NoError(err)
			recipientEthAddress := evm.GetEthAddress(recipientKey)

			// Create transaction
			nonce, err := ethClient.AcceptedNonceAt(e2e.DefaultContext(), ethAddress)
			require.NoError(err)
			tx := types.NewTx(&types.LegacyTx{
				Nonce:    nonce,
				GasPrice: gasPrice,
				Gas:      e2e.DefaultGasLimit,
				To:       &recipientEthAddress,
				Value:    common.Big0,
			})

			// Send the transaction and wait for acceptance
			signedTx := sign(tx)
			require.NoError(ethClient.SendTransaction(e2e.DefaultContext(), signedTx))
			_ = awaitTransaction(ethClient, signedTx)
		})

		e2e.CheckBootstrapIsPossible(e2e.Env.GetNetwork())
	})
})

// Waits for the transaction receipt to be issued and checks that it indicates success.
func awaitTransaction(ethClient ethclient.Client, signedTx *types.Transaction) *types.Receipt {
	require := require.New(ginkgo.GinkgoT())

	var receipt *types.Receipt
	e2e.Eventually(func() bool {
		var err error
		receipt, err = ethClient.TransactionReceipt(e2e.DefaultContext(), signedTx.Hash())
		if errors.Is(err, interfaces.NotFound) {
			return false // Transaction is still pending
		}
		require.NoError(err)
		return true
	}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see transaction acceptance before timeout")

	// Retrieve the contract address
	require.Equal(receipt.Status, types.ReceiptStatusSuccessful)
	return receipt
}
