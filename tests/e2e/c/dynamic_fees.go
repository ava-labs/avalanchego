// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"strings"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// This test uses the compiled bin for `hashing.sol` as
// well as its ABI contained in `hashing_contract.go`.

var _ = e2e.DescribeCChain("[Dynamic Fees]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	// Need a gas limit much larger than the standard 21_000 to enable
	// the contract to induce a gas price increase
	const largeGasLimit = uint64(8_000_000)

	// TODO(marun) What is the significance of this value?
	gasTip := big.NewInt(1000 * params.GWei)

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
		ethAddress := evm.GetEthAddress(key)

		tc.By("initializing a coreth client")
		node := privateNetwork.Nodes[0]
		nodeURI := tmpnet.NodeURI{
			NodeID: node.NodeID,
			URI:    node.URI,
		}
		ethClient := e2e.NewEthClient(tc, nodeURI)

		tc.By("initializing a transaction signer")
		cChainID, err := ethClient.ChainID(tc.DefaultContext())
		require.NoError(err)
		signer := types.NewEIP155Signer(cChainID)
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
			receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)

			contractAddress = receipt.ContractAddress
		})

		var gasPrice *big.Int
		tc.By("calling the expensive contract repeatedly until a gas price increase is detected", func() {
			// Evaluate the bytes representation of the contract
			hashingABI, err := abi.JSON(strings.NewReader(hashingABIJson))
			require.NoError(err)
			contractData, err := hashingABI.Pack("hashIt")
			require.NoError(err)

			var initialGasPrice *big.Int
			tc.Eventually(func() bool {
				// Check the gas price
				var err error
				gasPrice, err = ethClient.SuggestGasPrice(tc.DefaultContext())
				require.NoError(err)
				if initialGasPrice == nil {
					initialGasPrice = gasPrice
					tc.Outf("{{blue}}initial gas price is %v{{/}}\n", initialGasPrice)
				} else if gasPrice.Cmp(initialGasPrice) > 0 {
					// Gas price has increased
					tc.Outf("{{blue}}gas price has increased to %v{{/}}\n", gasPrice)
					return true
				}

				// Create the transaction
				nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
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
				tc.Outf("{{blue}}.{{/}}")
				return initialGasPrice.Cmp(gasPrice) > 0
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
			tc.Outf("\n{{blue}}gas price has decreased to %v{{/}}\n", gasPrice)
		})

		tc.By("sending funds at the current gas price", func() {
			// Create a recipient address
			var (
				recipientKey        = e2e.NewPrivateKey(tc)
				recipientEthAddress = evm.GetEthAddress(recipientKey)
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
