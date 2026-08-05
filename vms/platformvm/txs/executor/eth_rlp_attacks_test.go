// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	ethcommon "github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Outer credentials are a serialized unbounded slice that nothing else reads,
// so a tx could be padded to hundreds of kilobytes at a fixed fee and two of
// them would exceed the block codec limit, halting block production. An
// EthRLPTx must therefore carry none.
func TestEthRLPTxRejectsCredentialPadding(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	honest := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), units.Avax, defaultEthGasLimit,
		ethChainID(env), 0, nil)
	padded := &txs.Tx{
		Unsigned: &txs.EthRLPTx{RLP: honest.Unsigned.(*txs.EthRLPTx).RLP},
		Creds: []verify.Verifiable{
			&secp256k1fx.Credential{Sigs: make([][secp256k1.SignatureLen]byte, 3000)},
		},
	}
	require.NoError(padded.Initialize(txs.Codec))
	require.Greater(len(padded.Bytes()), 100_000, "the padding is what makes this an attack")

	_, _, _, err = StandardTx(&env.backend, feeCalculator, padded, onAcceptState)
	require.ErrorIs(err, errEthCredentials)
}

// An eth tx's gas is the limit it signed, and that is the number every part of
// the system uses. Padding the tx cannot change what it pays, and complexity
// dimensions are not a second path to a different answer.
func TestEthRLPTxGasIsTheSignedLimit(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Latest)
	weights := env.config.DynamicFeeConfig.Weights

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	for _, gasLimit := range []uint64{21_000, 100_000, 5_000_000} {
		tx := newSignedEthTx(t, key, 0, ids.GenerateTestShortID(), units.Avax, gasLimit,
			defaultFeeCapWei, ethChainID(env), 0, nil)
		txGas, err := txfee.TxGas(tx.Unsigned, weights)
		require.NoError(err)
		require.Equal(gas.Gas(gasLimit), txGas)
	}

	// The complexity visitor refuses eth txs, so nothing can accidentally
	// price one by its dimensions instead.
	tx := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), units.Avax, defaultEthGasLimit,
		ethChainID(env), 0, nil)
	_, err = txfee.TxComplexity(tx.Unsigned)
	require.ErrorIs(err, txfee.ErrUnsupportedTx)
}

// The envelope bound eth_estimateGas prices with must never be below what an
// extreme-but-valid tx actually serializes to, or the estimate would understate
// the gas the executor charges and wallets would produce rejected txs.
func TestEthRLPEnvelopeBound(t *testing.T) {
	require := require.New(t)
	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Every field at its maximum width: a uint64 chain ID, nonce and gas, and
	// uint256 value and fee caps. Nothing valid can serialize larger.
	maxU256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maxChainID := new(big.Int).SetUint64(math.MaxUint64)
	to := ethcommon.Address(ids.GenerateTestShortID())

	for _, calldataLen := range []int{0, 4, 100, 1_000, 100_000} {
		signed := ethtypes.MustSignNewTx(
			key.ToECDSA(),
			ethtypes.LatestSignerForChainID(maxChainID),
			&ethtypes.DynamicFeeTx{
				ChainID:   maxChainID,
				Nonce:     math.MaxUint64,
				GasTipCap: maxU256,
				GasFeeCap: maxU256,
				Gas:       math.MaxUint64,
				To:        &to,
				Value:     maxU256,
				Data:      make([]byte, calldataLen),
			},
		)
		raw, err := signed.MarshalBinary()
		require.NoError(err)
		require.LessOrEqual(len(raw)-calldataLen, txs.MaxEthRLPEnvelopeBytes,
			"envelope exceeded the derived bound with %d bytes of calldata", calldataLen)
	}
}

// A tx at the maximum nonce would store nonce+1 == 0, resetting the account's
// replay protection and making every previously accepted tx valid again.
func TestEthRLPTxRejectsMaxNonce(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	first := newSignedEthTransfer(t, key, 0, recipient, units.Avax, defaultEthGasLimit, ethChainID(env), 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, first, onAcceptState)
	require.NoError(err)

	wrap := newSignedEthTransfer(t, key, math.MaxUint64, recipient, units.Avax, defaultEthGasLimit,
		ethChainID(env), 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, wrap, onAcceptState)
	require.ErrorIs(err, txs.ErrNonceTooLarge)

	// The nonce did not move, so the first tx stays unreplayable.
	next, err := onAcceptState.GetNextNonce(sender)
	require.NoError(err)
	require.Equal(uint64(1), next)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, first, onAcceptState)
	require.ErrorIs(err, errStaleNonce)
}

// UTXO IDs are grindable offline, so any ID-first selection order lets an
// attacker plant cheap dust that permanently displaces a victim's real funds.
// Amount-first ordering means dust can never crowd out value.
func TestEthRLPTxDustCannotBrickAnAccount(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	victim := ids.ShortID(key.PublicKey().EthAddress())

	// The victim's balance is a single large UTXO.
	var bigID ids.ID
	for {
		txID := ids.GenerateTestID()
		if id := inputID(txID, 0); id.Compare(ids.Empty) > 0 {
			bigID = id
			fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, txID, victim, 1000*units.Avax)
			break
		}
	}

	// The attacker plants more dust UTXOs than one tx may consume, every one of
	// them with an ID that sorts below the victim's.
	planted := 0
	for planted < txs.MaxEthRLPTxInputs+8 {
		txID := ids.GenerateTestID()
		if inputID(txID, 0).Compare(bigID) >= 0 {
			continue
		}
		fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, txID, victim, 1)
		planted++
	}

	tx := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), units.Avax, defaultEthGasLimit,
		ethChainID(env), 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err, "dust displaced the victim's real balance")

	// The large UTXO was spent and the change came back, so the account is
	// still usable rather than frozen.
	_, err = onAcceptState.GetUTXO(bigID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Selection takes the largest UTXOs first, ties broken by ID, so it is a pure
// function of the UTXO set and never of insertion order.
func TestEthRLPTxSelectsLargestUTXOsFirst(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())

	// One 10 AVAX UTXO and many 1 AVAX UTXOs: a 6 AVAX spend must consume the
	// big one alone.
	bigTxID := ids.GenerateTestID()
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, bigTxID, sender, 10*units.Avax)
	smallIDs := make([]ids.ID, 0, 5)
	for i := 0; i < 5; i++ {
		txID := ids.GenerateTestID()
		fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, txID, sender, units.Avax)
		smallIDs = append(smallIDs, inputID(txID, 0))
	}

	tx := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), 6*units.Avax, defaultEthGasLimit,
		ethChainID(env), 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	_, err = onAcceptState.GetUTXO(inputID(bigTxID, 0))
	require.ErrorIs(err, database.ErrNotFound, "the largest UTXO should be spent first")
	for _, id := range smallIDs {
		_, err := onAcceptState.GetUTXO(id)
		require.NoError(err, "small UTXOs should be untouched")
	}
}

// A zero-value self-send is how a wallet cancels a pending tx, and future
// selectors need not carry value, so a zero value is legal at the tx level and
// required only by the selectors that stake it.
func TestEthRLPTxZeroValueCancel(t *testing.T) {
	require := require.New(t)
	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	// The cancel: a zero-value self-send at the stuck nonce.
	cancel := newSignedEthTransfer(t, key, 0, sender, 0, defaultEthGasLimit, ethChainID(env), 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, cancel, onAcceptState)
	require.NoError(err)

	// It burned the fee and advanced the nonce, and produced no zero-value UTXO.
	next, err := onAcceptState.GetNextNonce(sender)
	require.NoError(err)
	require.Equal(uint64(1), next)
	_, err = onAcceptState.GetUTXO(inputID(cancel.ID(), 0))
	require.ErrorIs(err, database.ErrNotFound)

	// Staking calls still require value.
	stake := newSignedEthStake(t, env, key, 1, 0,
		delegateCalldata(ids.GenerateTestNodeID(), 1700000000))
	require.ErrorIs(stake.Unsigned.SyntacticVerify(env.ctx), txs.ErrStakeValueRequired)
}

// The network ID is not part of an eth tx's signed preimage, so the chain ID
// must separate networks: a tx signed for one network must be invalid on every
// other, or a devnet rehearsal is a valid mainnet tx.
func TestEthRLPTxChainIDSeparatesNetworks(t *testing.T) {
	require := require.New(t)

	mainnet := txs.EthRLPChainID(constants.MainnetID)
	fuji := txs.EthRLPChainID(constants.FujiID)
	local := txs.EthRLPChainID(constants.LocalID)
	require.NotEqual(mainnet, fuji)
	require.NotEqual(mainnet, local)
	require.NotEqual(fuji, local)

	env, onAcceptState, feeCalculator := ethFeeEnv(t, upgradetest.Latest, 1)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)

	// A tx signed for another network does not execute here.
	otherNetwork := new(big.Int).Add(ethChainID(env), big.NewInt(1))
	foreign := newSignedEthTransfer(t, key, 0, ids.GenerateTestShortID(), units.Avax, 10*units.Avax,
		otherNetwork, 0, nil)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, foreign, onAcceptState)
	require.ErrorIs(err, txs.ErrWrongEthChainID)

	// Verification without a context cannot decide the chain ID, so it fails
	// closed rather than defaulting to some network.
	require.ErrorIs(
		(&txs.EthRLPTx{RLP: foreign.Unsigned.(*txs.EthRLPTx).RLP}).SyntacticVerify(nil),
		txs.ErrMissingContext,
	)
}
