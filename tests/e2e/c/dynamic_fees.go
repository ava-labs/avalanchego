// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"strings"
	"time"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/cortina"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

// This test uses the compiled bytecode for `consume_gas.sol` as well as its ABI
// contained in `consume_gas.go`.

var _ = e2e.DescribeCChain("[Dynamic Fees]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	const (
		// maxFeePerGas is the maximum fee that transactions issued by this test
		// will be willing to pay. The actual value doesn't really matter, it
		// just needs to be higher than the `targetGasPrice` calculated below.
		maxFeePerGas = 1000 * params.GWei
		// minFeePerGas is the minimum fee that transactions issued by this test
		// will pay. The mempool enforces that this value is non-zero.
		minFeePerGas = 1 * params.Wei

		// expectedGasPriceIncreaseNumerator/expectedGasPriceIncreaseDenominator
		// is the multiplier that the gas price is attempted to reach. So if the
		// initial gas price is 5 GWei, the test will attempt to increase the
		// gas price to 6 GWei.
		expectedGasPriceIncreaseNumerator   = 6
		expectedGasPriceIncreaseDenominator = 5
	)
	var (
		bigExpectedGasPriceIncreaseNumerator         = big.NewInt(expectedGasPriceIncreaseNumerator)
		bigExpectedGasPriceIncreaseDenominator       = big.NewInt(expectedGasPriceIncreaseDenominator)
		bigExpectedGasPriceIncreaseDenominatorMinus1 = big.NewInt(expectedGasPriceIncreaseDenominator - 1)

		gasFeeCap = big.NewInt(maxFeePerGas)
		gasTipCap = big.NewInt(minFeePerGas)
	)

	ginkgo.It("should ensure that the gas price is affected by load", func() {
		tc.By("creating a new private network to ensure isolation from other tests")
		env := e2e.GetEnv(tc)
		publicNetwork := env.GetNetwork()

		privateNetwork := tmpnet.NewDefaultNetwork("avalanchego-e2e-dynamic-fees")
		// Copy over the defaults from the normal test suite to include settings
		// like the upgrade config.
		privateNetwork.DefaultFlags = tmpnet.FlagsMap{}
		privateNetwork.DefaultFlags.SetDefaults(publicNetwork.DefaultFlags)
		env.StartPrivateNetwork(privateNetwork)

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
			URI:    node.GetAccessibleURI(),
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

		// gasLimit is the maximum amount of gas that can be used in a block.
		var gasLimit uint64
		tc.By("checking if Fortuna is activated", func() {
			infoClient := info.NewClient(nodeURI.URI)
			upgrades, err := infoClient.Upgrades(tc.DefaultContext())
			require.NoError(err)

			now := time.Now()
			if upgrades.IsFortunaActivated(now) {
				gasLimit = acp176.MinMaxCapacity
			} else {
				gasLimit = cortina.GasLimit
			}
		})
		tc.Log().Info("set gas limit",
			zap.Uint64("gasLimit", gasLimit),
		)

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

		initialGasPrice, err := ethClient.EstimateBaseFee(tc.DefaultContext())
		require.NoError(err)

		targetGasPrice := new(big.Int).Set(initialGasPrice)
		targetGasPrice.Mul(targetGasPrice, bigExpectedGasPriceIncreaseNumerator)
		targetGasPrice.Add(targetGasPrice, bigExpectedGasPriceIncreaseDenominatorMinus1)
		targetGasPrice.Div(targetGasPrice, bigExpectedGasPriceIncreaseDenominator)

		tc.Log().Info("initializing gas prices",
			zap.Stringer("initialPrice", initialGasPrice),
			zap.Stringer("targetPrice", targetGasPrice),
		)

		tc.By("calling the contract repeatedly until a sufficient gas price increase is detected", func() {
			// Evaluate the bytes representation of the contract
			hashingABI, err := abi.JSON(strings.NewReader(consumeGasABIJson))
			require.NoError(err)
			contractData, err := hashingABI.Pack(consumeGasFunction)
			require.NoError(err)

			tc.Eventually(func() bool {
				// Check the gas price
				gasPrice, err := ethClient.EstimateBaseFee(tc.DefaultContext())
				require.NoError(err)

				// If the gas price has increased, stop the loop.
				if gasPrice.Cmp(targetGasPrice) >= 0 {
					tc.Log().Info("gas price has increased",
						zap.Stringer("initialPrice", initialGasPrice),
						zap.Stringer("targetPrice", targetGasPrice),
						zap.Stringer("newPrice", gasPrice),
					)
					return true
				}

				tc.Log().Info("gas price hasn't sufficiently increased",
					zap.Stringer("initialPrice", initialGasPrice),
					zap.Stringer("newPrice", gasPrice),
					zap.Stringer("targetPrice", targetGasPrice),
				)

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
				receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
				// The transaction should have run out of gas
				require.Equal(types.ReceiptStatusFailed, receipt.Status)

				// The gas price will be checked at the start of the next iteration
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price increase before timeout")
		})

		// Create a recipient address
		var (
			recipientKey        = e2e.NewPrivateKey(tc)
			recipientEthAddress = recipientKey.EthAddress()
		)
		tc.By("sending small transactions until a sufficient gas price decrease is detected", func() {
			tc.Eventually(func() bool {
				// Check the gas price
				gasPrice, err := ethClient.EstimateBaseFee(tc.DefaultContext())
				require.NoError(err)

				// If the gas price has decreased, stop the loop.
				if gasPrice.Cmp(initialGasPrice) <= 0 {
					tc.Log().Info("gas price has decreased",
						zap.Stringer("initialPrice", initialGasPrice),
						zap.Stringer("newPrice", gasPrice),
					)
					return true
				}

				tc.Log().Info("gas price hasn't sufficiently decreased",
					zap.Stringer("initialPrice", initialGasPrice),
					zap.Stringer("newPrice", gasPrice),
				)

				// Create the transaction
				nonce, err := ethClient.AcceptedNonceAt(tc.DefaultContext(), ethAddress)
				require.NoError(err)
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   cChainID,
					Nonce:     nonce,
					GasTipCap: gasTipCap,
					GasFeeCap: gasFeeCap,
					Gas:       e2e.DefaultGasLimit,
					To:        &recipientEthAddress,
				})

				// Send the transaction and wait for acceptance
				signedTx := sign(tx)
				receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
				require.Equal(types.ReceiptStatusSuccessful, receipt.Status)

				// The gas price will be checked at the start of the next iteration
				return false
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "failed to see gas price decrease before timeout")
		})

		_ = e2e.CheckBootstrapIsPossible(tc, privateNetwork)
	})
})
