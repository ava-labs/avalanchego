// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
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

// The fee no longer moves with the input count, so the target is fixed: the
// walk must still take a second input when the first cannot cover value plus
// that fixed fee, and must charge the same fee either way.
func TestEthRLPTxTakesASecondInputWhenTheFirstIsShort(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())

	const rlpLen = 132
	spender := NewEthSpender(
		onAcceptState,
		env.config.DynamicFeeConfig.Weights,
		env.ctx.AVAXAssetID,
		feeCalculator,
	)
	// Sign enough gas for two inputs, so the budget is not what limits this.
	twoInputGas, err := spender.MinGasFor(rlpLen, 2)
	require.NoError(err)
	txFee, err := feeCalculator.CalculateFeeForGas(twoInputGas)
	require.NoError(err)

	// The first input is one nAVAX short of value plus fee; the second covers
	// the remainder exactly.
	const value = 10 * units.Avax
	firstAmount := value + txFee - 1
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, firstAmount)
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 1)

	spend, err := spender.SelectInputs(sender, value, rlpLen, uint64(twoInputGas))
	require.NoError(err)
	require.Len(spend.Consumed, 2)
	require.Equal(txFee, spend.Fee)
	require.Equal(firstAmount+1, spend.Total)
	require.GreaterOrEqual(spend.Total, value+spend.Fee)

	// One nAVAX less and nothing covers it.
	env2, onAcceptState2, feeCalculator2 := ethFeeEnvMainnetWeights(t, 1)
	env2.ctx.Lock.Lock()
	defer env2.ctx.Lock.Unlock()
	fundEthAddress(onAcceptState2, env2.ctx.AVAXAssetID, ids.GenerateTestID(), sender, firstAmount)
	spender2 := NewEthSpender(
		onAcceptState2,
		env2.config.DynamicFeeConfig.Weights,
		env2.ctx.AVAXAssetID,
		feeCalculator2,
	)
	_, err = spender2.SelectInputs(sender, value, rlpLen, uint64(twoInputGas))
	require.ErrorIs(err, errEthInsufficientFunds)
}

// The fee charged is the fee selection computed, so the fee is a pure function
// of the tx bytes and the price.
func TestEthRLPTxChargedFeeMatchesSelection(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	for i := 0; i < 3; i++ {
		fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 10*units.Avax)
	}

	const value = 15 * units.Avax
	tx := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), value, 100_000,
		defaultFeeCapWei, ethChainID(env), 0, nil)
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
	require.Len(spend.Consumed, 2, "two of the three 10 AVAX UTXOs cover 15 plus fee")

	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	change, err := onAcceptState.GetUTXO(inputID(tx.ID(), 1))
	require.NoError(err)
	require.Equal(spend.Fee, spend.Total-value-amountOf(change))

	// And that fee is the signed limit's fee.
	wantFee, err := feeCalculator.CalculateFeeForGas(gas.Gas(100_000))
	require.NoError(err)
	require.Equal(wantFee, spend.Fee)
}

// The fee is the signed gas limit times the price, so it scales with the limit.
// That is what removes any leverage over the fee market: an attacker who wants
// to move Excess pays for every unit of it.
func TestEthRLPTxFeeScalesWithSignedGasLimit(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	spender := NewEthSpender(
		onAcceptState,
		env.config.DynamicFeeConfig.Weights,
		env.ctx.AVAXAssetID,
		feeCalculator,
	)
	const rlpLen = 132
	honest, err := spender.MinGasFor(rlpLen, 1)
	require.NoError(err)

	// Ten times the gas costs ten times the fee, exactly.
	honestFee, err := feeCalculator.CalculateFeeForGas(honest)
	require.NoError(err)
	greedyFee, err := feeCalculator.CalculateFeeForGas(honest * 10)
	require.NoError(err)
	require.Equal(honestFee*10, greedyFee)

	// And what a tx is charged is what its signed limit costs, whatever it
	// actually needed.
	padded := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), units.Avax,
		uint64(honest)*3/2, defaultFeeCapWei, ethChainID(env), 0, nil)
	unsigned := padded.Unsigned.(*txs.EthRLPTx)
	require.NoError(unsigned.SyntacticVerify(env.ctx))
	txGas, err := txfee.TxGas(unsigned, env.config.DynamicFeeConfig.Weights)
	require.NoError(err)
	require.Equal(gas.Gas(unsigned.Parsed.Gas()), txGas)

	expectedFee, err := feeCalculator.CalculateFeeForGas(txGas)
	require.NoError(err)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, padded, onAcceptState)
	require.NoError(err)

	change, err := onAcceptState.GetUTXO(inputID(padded.ID(), 1))
	require.NoError(err)
	charged := 100*units.Avax - units.Avax - amountOf(change)
	require.Equal(expectedFee, charged, "charged the signed limit, not what it used")
}

// The wallet flows that matter in practice: our exact estimate, a padded limit
// like Rabby signs, and a dapp hardcoding the EVM transfer constant. All three
// must land; the padded ones simply overpay.
func TestEthRLPTxWalletGasLimitFlows(t *testing.T) {
	const rlpLen = 132
	for _, tt := range []struct {
		name     string
		gasLimit func(exact uint64) uint64
	}{
		{name: "exact estimate", gasLimit: func(exact uint64) uint64 { return exact }},
		{name: "padded 1.5x", gasLimit: func(exact uint64) uint64 { return exact * 3 / 2 }},
		{name: "hardcoded 21000", gasLimit: func(uint64) uint64 { return 21_000 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			env, onAcceptState, feeCalculator := ethFeeEnvMainnetWeights(t, 1)
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			key, err := secp256k1.NewPrivateKey()
			require.NoError(err)
			sender := ids.ShortID(key.PublicKey().EthAddress())
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

			spender := NewEthSpender(
				onAcceptState,
				env.config.DynamicFeeConfig.Weights,
				env.ctx.AVAXAssetID,
				feeCalculator,
			)
			exact, err := spender.MinGasFor(rlpLen, 1)
			require.NoError(err)

			gasLimit := tt.gasLimit(uint64(exact))
			tx := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), units.Avax,
				gasLimit, defaultFeeCapWei, ethChainID(env), 0, nil)
			_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
			require.NoError(err)

			// Charged exactly the signed limit.
			wantFee, err := feeCalculator.CalculateFeeForGas(gas.Gas(gasLimit))
			require.NoError(err)
			change, err := onAcceptState.GetUTXO(inputID(tx.ID(), 1))
			require.NoError(err)
			require.Equal(wantFee, 100*units.Avax-units.Avax-amountOf(change))
		})
	}
}
