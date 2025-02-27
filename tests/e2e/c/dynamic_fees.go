// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"strings"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/cortina"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// This test uses the compiled bytecode for `consume_gas.sol` as well as its ABI
// contained in `consume_gas.go`.

var _ = e2e.DescribeCChain("[Dynamic Fees]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const (
		// gasLimit is the maximum amount of gas that can be used in a block.
		gasLimit = cortina.GasLimit // acp176.MinMaxCapacity
		// maxFeePerGas is the maximum fee that transactions issued by this test
		// will be willing to pay.
		maxFeePerGas = 1000 * params.GWei
		// minFeePerGas is the minimum fee that transactions issued by this test
		// will pay.
		minFeePerGas = 1 * params.Wei
	)
	var (
		gasFeeCap = big.NewInt(maxFeePerGas)
		gasTipCap = big.NewInt(minFeePerGas)
	)

	ginkgo.It("should ensure that the gas price is affected by load", func() {
		tc.By("creating a new private network to ensure isolation from other tests")
		privateNetwork := tmpnet.NewDefaultNetwork("avalanchego-e2e-dynamic-fees")
		e2e.GetEnv(tc).StartPrivateNetwork(privateNetwork)

		// Avoid emitting a spec-scoped metrics link for the shared
		// network since the link emitted by the start of the private
		// network is more relevant.
		//
		// TODO(marun) Make this implicit to the start of a private network
		e2e.EmitMetricsLink = false

		tc.By("allocating a pre-funded key")
		key := privateNetwork.PreFundedKeys[0]
		ethAddress := key.EthAddress()

		tc.By("initializing a coreth client")
		node := privateNetwork.Nodes[0]
		nodeURI := tmpnet.NodeURI{
			NodeID: node.NodeID,
			URI:    e2e.GetLocalURI(tc, node),
		}
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("initializing a transaction signer")
		cChainID, err := ethClient.ChainID(tc.DefaultContext())
		require.NoError(err)
		signer := types.NewLondonSigner(cChainID)
		ecdsaKey := key.ToECDSA()
		sign := func(tx *types.Transaction) *types.Transaction {
			signedTx, err := types.SignTx(tx, signer, ecdsaKey)
			require.NoError(err)
			return signedTx
		}

		var contractAddress common.Address
		tc.By("deploying an expensive contract", func() {
			// Create transaction
			nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
			require.NoError(err)
			compiledContract := common.Hex2Bytes(consumeGasCompiledContract)
			tx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   cChainID,
				Nonce:     nonce,
				GasTipCap: gasTipCap,
				GasFeeCap: gasFeeCap,
				Gas:       gasLimit,
				To:        nil, // contract creation
				Data:      compiledContract,
			})

			// Send the transaction and wait for acceptance
			signedTx := sign(tx)
			receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
			require.Equal(types.ReceiptStatusSuccessful, receipt.Status)

			contractAddress = receipt.ContractAddress
		})

		var gasPrice *big.Int
		tc.By("calling the expensive contract repeatedly until a gas price increase is detected", func() {
			// Evaluate the bytes representation of the contract
			hashingABI, err := abi.JSON(strings.NewReader(consumeGasABIJson))
			require.NoError(err)
			contractData, err := hashingABI.Pack(consumeGasFunction)
			require.NoError(err)

			var initialGasPrice *big.Int
			tc.Eventually(func() bool {
				// Check the gas price
				gasPrice, err = ethClient.SuggestGasPrice(tc.DefaultContext())
				require.NoError(err)

				// If this is the first iteration, record the initial gas price.
				if initialGasPrice == nil {
					initialGasPrice = gasPrice
					tc.Log().Info("initial gas price",
						zap.Stringer("price", initialGasPrice),
					)
				}

				// If the gas price has increased, stop the loop.
				if gasPrice.Cmp(initialGasPrice) > 0 {
					tc.Log().Info("gas price has increased",
						zap.Stringer("initialPrice", initialGasPrice),
						zap.Stringer("newPrice", gasPrice),
					)
					return true
				}

				// Create the transaction
				nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
				require.NoError(err)
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   cChainID,
					Nonce:     nonce,
					GasTipCap: gasTipCap,
					GasFeeCap: gasFeeCap,
					Gas:       gasLimit,
					To:        &contractAddress,
					Data:      contractData,
				})

				// Send the transaction and wait for acceptance
				signedTx := sign(tx)
				_ = e2e.SendEthTransaction(tc, ethClient, signedTx)

				// The gas price will be checked at the start of the next iteration
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price increase before timeout")
		})

		tc.By("waiting for the gas price to decrease...", func() {
			initialGasPrice := gasPrice
			tc.Eventually(func() bool {
				var err error
				gasPrice, err = ethClient.SuggestGasPrice(tc.DefaultContext())
				require.NoError(err)
				return initialGasPrice.Cmp(gasPrice) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
			tc.Log().Info("gas price has decreased",
				zap.Stringer("initialPrice", initialGasPrice),
				zap.Stringer("newPrice", gasPrice),
			)
		})

		tc.By("sending funds at the current gas price", func() {
			// Create a recipient address
			var (
				recipientKey        = e2e.NewPrivateKey(tc)
				recipientEthAddress = recipientKey.EthAddress()
			)

			// Create transaction
			nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
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
			_ = e2e.SendEthTransaction(tc, ethClient, signedTx)
		})

		_ = e2e.CheckBootstrapIsPossible(tc, privateNetwork)
	})
})
