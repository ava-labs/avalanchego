// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

// mainnetWeights are the production gas weights, shared by every network.
var mainnetWeights = gas.Dimensions{
	gas.Bandwidth: 1,
	gas.DBRead:    1_000,
	gas.DBWrite:   1_000,
	gas.Compute:   4,
}

// A transfer is priced for the inputs it consumes, so the common one-input send
// costs about 6,932 gas at production weights and each extra input adds 2,000.
// This is what keeps a dapp that hardcodes gas: 21000 working.
func TestEthRLPTxGasPerInputCount(t *testing.T) {
	require := require.New(t)

	const rlpLen = 132 // a plain transfer's serialized length
	oneInput, err := txfee.EthRLPTxComplexity(rlpLen, 1).ToGas(mainnetWeights)
	require.NoError(err)
	twoInputs, err := txfee.EthRLPTxComplexity(rlpLen, 2).ToGas(mainnetWeights)
	require.NoError(err)

	// 2 reads + 4 writes + 200 compute + 132 bandwidth.
	require.Equal(gas.Gas(6_932), oneInput)
	require.Equal(gas.Gas(8_932), twoInputs)
	require.Equal(gas.Gas(2_000), twoInputs-oneInput)

	// The EVM's canonical transfer limit covers eight inputs, not seven or nine.
	const evmTransferGas = 21_000
	eight, err := txfee.EthRLPTxComplexity(rlpLen, 8).ToGas(mainnetWeights)
	require.NoError(err)
	nine, err := txfee.EthRLPTxComplexity(rlpLen, 9).ToGas(mainnetWeights)
	require.NoError(err)
	require.LessOrEqual(uint64(eight), uint64(evmTransferGas))
	require.Greater(uint64(nine), uint64(evmTransferGas))
}

// A tx signed with the EVM's 21,000 transfer limit must execute, consuming as
// many inputs as that budget covers, and must be rejected with the actionable
// fragmentation error when the account needs more.
func TestEthRLPTxRespectsEVMTransferGasLimit(t *testing.T) {
	const evmTransferGas = 21_000

	// Eight 1 AVAX UTXOs, spending nearly all of it: fits the budget.
	t.Run("eight inputs fit", func(t *testing.T) {
		require := require.New(t)
		env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
		env.ctx.Lock.Lock()
		defer env.ctx.Lock.Unlock()

		key, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		sender := ids.ShortID(key.PublicKey().EthAddress())
		for i := 0; i < 8; i++ {
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, units.Avax)
		}

		tx := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), 8*units.Avax-units.MilliAvax,
			evmTransferGas, defaultFeeCapWei, ethChainID(env), 0, nil)
		_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
		require.NoError(err)
	})

	// Nine UTXOs needed: over the budget, so the user gets the actionable error.
	t.Run("nine inputs need a higher limit", func(t *testing.T) {
		require := require.New(t)
		env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
		env.ctx.Lock.Lock()
		defer env.ctx.Lock.Unlock()

		key, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		sender := ids.ShortID(key.PublicKey().EthAddress())
		for i := 0; i < 9; i++ {
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, units.Avax)
		}

		tx := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), 9*units.Avax-units.MilliAvax,
			evmTransferGas, defaultFeeCapWei, ethChainID(env), 0, nil)
		_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
		require.ErrorIs(err, errEthGasLimitTooLowForInputs)

		// Raising the limit is the documented fix, and it works.
		tx = newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), 9*units.Avax-units.MilliAvax,
			100_000, defaultFeeCapWei, ethChainID(env), 0, nil)
		_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
		require.NoError(err)
	})
}

// Adding an input raises the fee, which raises the requirement again. The walk
// must settle on an input count whose own fee it can still cover, never one
// chosen against a stale requirement.
func TestEthRLPTxFeeConvergesOnTheMarginalInput(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())

	spender := NewEthSpender(
		onAcceptState,
		env.config.DynamicFeeConfig.Weights,
		env.ctx.AVAXAssetID,
		feeCalculator,
	)
	const rlpLen = 132
	oneGas, err := spender.gasFor(rlpLen, 1)
	require.NoError(err)
	twoGas, err := spender.gasFor(rlpLen, 2)
	require.NoError(err)
	oneFee, err := feeCalculator.CalculateFeeForGas(oneGas)
	require.NoError(err)
	twoFee, err := feeCalculator.CalculateFeeForGas(twoGas)
	require.NoError(err)
	require.Greater(twoFee, oneFee)

	// Fund so that one input covers value plus the ONE-input fee exactly minus
	// one nAVAX: one input is short, and the second input must cover both the
	// shortfall and the fee increase it causes.
	const value = 10 * units.Avax
	firstAmount := value + oneFee - 1
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, firstAmount)
	// The second input is worth exactly the shortfall plus the fee delta, the
	// tightest case that can still succeed.
	secondAmount := 1 + (twoFee - oneFee)
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, secondAmount)

	spend, err := spender.SelectInputs(sender, value, rlpLen, 1_000_000)
	require.NoError(err)
	require.Len(spend.Consumed, 2, "one input cannot cover its own fee here")
	require.Equal(twoFee, spend.Fee)
	require.Equal(firstAmount+secondAmount, spend.Total)

	// The charge is exactly what the stopping condition tested: no off-by-one.
	require.GreaterOrEqual(spend.Total, value+spend.Fee)

	// One nAVAX less in the second input and it cannot converge at all.
	env2, onAcceptState2, feeCalculator2 := ethFeeEnv(t, upgradetest.Latest, 1)
	env2.ctx.Lock.Lock()
	defer env2.ctx.Lock.Unlock()
	fundEthAddress(onAcceptState2, env2.ctx.AVAXAssetID, ids.GenerateTestID(), sender, firstAmount)
	fundEthAddress(onAcceptState2, env2.ctx.AVAXAssetID, ids.GenerateTestID(), sender, secondAmount-1)
	spender2 := NewEthSpender(
		onAcceptState2,
		env2.config.DynamicFeeConfig.Weights,
		env2.ctx.AVAXAssetID,
		feeCalculator2,
	)
	_, err = spender2.SelectInputs(sender, value, rlpLen, 1_000_000)
	require.ErrorIs(err, errEthInsufficientFunds)
}

// The fee charged must equal the fee the selection walk stopped on, which is
// what makes the fee a pure function of (tx bytes, state).
func TestEthRLPTxChargedFeeMatchesSelection(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	// Three equal UTXOs, a spend that needs two of them.
	for i := 0; i < 3; i++ {
		fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 10*units.Avax)
	}

	const value = 15 * units.Avax
	tx := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), value, 100_000, ethChainID(env), 0, nil)
	unsigned := tx.Unsigned.(*txs.EthRLPTx)
	require.NoError(unsigned.SyntacticVerify(env.ctx))

	spender := NewEthSpender(
		onAcceptState,
		env.config.DynamicFeeConfig.Weights,
		env.ctx.AVAXAssetID,
		feeCalculator,
	)
	spend, err := spender.SelectInputs(sender, value, len(unsigned.RLP), 100_000)
	require.NoError(err)
	require.Len(spend.Consumed, 2)

	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	// The change output proves the exact fee charged.
	change, err := onAcceptState.GetUTXO(inputID(tx.ID(), 1))
	require.NoError(err)
	charged := spend.Total - value - amountOf(change)
	require.Equal(spend.Fee, charged)

	// And that fee is the two-input fee, not the one-input or ceiling fee.
	twoGas, err := spender.gasFor(len(unsigned.RLP), 2)
	require.NoError(err)
	twoFee, err := feeCalculator.CalculateFeeForGas(twoGas)
	require.NoError(err)
	require.Equal(twoFee, charged)
}

// The pre-execution reservation must never be below what execution charges, or
// a block could consume more gas than it accounted for.
func TestEthRLPTxReservationCoversAnyExecution(t *testing.T) {
	require := require.New(t)

	const rlpLen = 200
	reserved, err := txfee.EthRLPTxMaxComplexity(rlpLen).ToGas(mainnetWeights)
	require.NoError(err)
	for n := 1; n <= txs.MaxEthRLPTxInputs; n++ {
		actual, err := txfee.EthRLPTxComplexity(rlpLen, n).ToGas(mainnetWeights)
		require.NoError(err)
		require.LessOrEqual(uint64(actual), uint64(reserved))
	}
}
