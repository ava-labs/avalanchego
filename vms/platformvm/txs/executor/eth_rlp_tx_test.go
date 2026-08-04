// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	ethtypes "github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethcommon "github.com/ava-labs/libevm/common"
)

func newSignedEthTransfer(
	t *testing.T,
	key *secp256k1.PrivateKey,
	nonce uint64,
	to ids.ShortID,
	amountNAVAX uint64,
	gasLimit uint64,
	chainID int64,
	extraWei int64,
	calldata []byte,
) *txs.Tx {
	t.Helper()

	value := new(big.Int).Mul(new(big.Int).SetUint64(amountNAVAX), txs.WeiPerNAVAX)
	value.Add(value, big.NewInt(extraWei))
	ethTo := ethcommon.Address(to)
	signed := ethtypes.MustSignNewTx(
		key.ToECDSA(),
		ethtypes.LatestSignerForChainID(big.NewInt(chainID)),
		&ethtypes.DynamicFeeTx{
			ChainID:   big.NewInt(chainID),
			Nonce:     nonce,
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(1),
			Gas:       gasLimit,
			To:        &ethTo,
			Value:     value,
			Data:      calldata,
		},
	)
	rlp, err := signed.MarshalBinary()
	require.NoError(t, err)

	tx, err := txs.NewSigned(&txs.EthRLPTx{RLP: rlp}, txs.Codec, nil)
	require.NoError(t, err)
	return tx
}

func inputID(txID ids.ID, index uint32) ids.ID {
	utxoID := avax.UTXOID{TxID: txID, OutputIndex: index}
	return utxoID.InputID()
}

// fundEthAddress adds a spendable AVAX UTXO owned by [addr] to [chain].
func fundEthAddress(chain state.Chain, avaxAssetID ids.ID, txID ids.ID, addr ids.ShortID, amt uint64) {
	chain.AddUTXO(&avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID, OutputIndex: 0},
		Asset:  avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amt,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	})
}

func TestEthRLPTxTransfer(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()

	onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 10*units.Avax)

	tx := newSignedEthTransfer(t, key, 0, recipient, 3*units.Avax, 10*units.Avax, txs.EthRLPChainID, 0, nil)
	feeCalculator := fee.NewDynamicCalculator(gas.Dimensions{1, 1, 1, 1}, 1)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	// Recipient got exactly the amount.
	out, err := onAcceptState.GetUTXO(inputID(tx.ID(), 0))
	require.NoError(err)
	transferOut := out.Out.(*secp256k1fx.TransferOutput)
	require.Equal(3*units.Avax, transferOut.Amt)
	require.Equal([]ids.ShortID{recipient}, transferOut.OutputOwners.Addrs)

	// Change (minus the fee) came back to the sender.
	change, err := onAcceptState.GetUTXO(inputID(tx.ID(), 1))
	require.NoError(err)
	changeOut := change.Out.(*secp256k1fx.TransferOutput)
	require.Equal([]ids.ShortID{sender}, changeOut.OutputOwners.Addrs)
	fee := 7*units.Avax - changeOut.Amt
	require.NotZero(fee)
	require.Less(fee, units.Avax) // sanity: fee is a small fraction

	// Nonce advanced.
	next, err := onAcceptState.GetNextNonce(sender)
	require.NoError(err)
	require.Equal(uint64(1), next)
}

func TestEthRLPTxNonceRule(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()

	onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)
	feeCalculator := fee.NewDynamicCalculator(gas.Dimensions{1, 1, 1, 1}, 1)

	issue := func(nonce uint64) error {
		tx := newSignedEthTransfer(t, key, nonce, recipient, units.Avax, 10*units.Avax, txs.EthRLPChainID, 0, nil)
		_, _, _, err := StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
		return err
	}

	require.NoError(issue(0))                // first tx, nonce 0
	require.ErrorIs(issue(0), errStaleNonce) // replay rejected
	require.NoError(issue(5))                // gaps are allowed
	require.ErrorIs(issue(5), errStaleNonce) // equal rejected
	require.ErrorIs(issue(3), errStaleNonce) // lower rejected
	require.NoError(issue(6))                // strictly greater accepted
}

func TestEthRLPTxSelectionDeterminism(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()

	onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(err)

	// Three 5-AVAX UTXOs; a 7-AVAX transfer must consume exactly the two with
	// the lowest input IDs, leaving the highest untouched.
	utxoIDs := make([]ids.ID, 3)
	for i := range utxoIDs {
		txID := ids.GenerateTestID()
		fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, txID, sender, 5*units.Avax)
		utxoIDs[i] = inputID(txID, 0)
	}
	highest := utxoIDs[0]
	for _, id := range utxoIDs[1:] {
		if highest.Compare(id) < 0 {
			highest = id
		}
	}

	tx := newSignedEthTransfer(t, key, 0, recipient, 7*units.Avax, 10*units.Avax, txs.EthRLPChainID, 0, nil)
	feeCalculator := fee.NewDynamicCalculator(gas.Dimensions{1, 1, 1, 1}, 1)
	_, _, _, err = StandardTx(&env.backend, feeCalculator, tx, onAcceptState)
	require.NoError(err)

	for _, id := range utxoIDs {
		_, err := onAcceptState.GetUTXO(id)
		if id == highest {
			require.NoError(err, "highest-ID UTXO should not be selected")
		} else {
			require.ErrorIs(err, database.ErrNotFound)
		}
	}
}

func TestEthRLPTxSyntacticRejections(t *testing.T) {
	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	sender := ids.ShortID(key.PublicKey().EthAddress())
	recipient := ids.GenerateTestShortID()

	onAcceptState, err := state.NewDiff(lastAcceptedID, env, state.StakerAdditionAfterDeletionForbidden)
	require.NoError(t, err)
	fundEthAddress(onAcceptState, env.ctx.AVAXAssetID, ids.GenerateTestID(), sender, 100*units.Avax)
	feeCalculator := fee.NewDynamicCalculator(gas.Dimensions{1, 1, 1, 1}, 1)

	tests := []struct {
		name string
		tx   *txs.Tx
		err  error
	}{
		{
			name: "wrong chain ID",
			tx:   newSignedEthTransfer(t, key, 0, recipient, units.Avax, 10*units.Avax, txs.EthRLPChainID+1, 0, nil),
			err:  txs.ErrWrongEthChainID,
		},
		{
			name: "dust value",
			tx:   newSignedEthTransfer(t, key, 0, recipient, units.Avax, 10*units.Avax, txs.EthRLPChainID, 1, nil),
			err:  txs.ErrValueDust,
		},
		{
			name: "non-empty calldata",
			tx:   newSignedEthTransfer(t, key, 0, recipient, units.Avax, 10*units.Avax, txs.EthRLPChainID, 0, []byte{0x01}),
			err:  txs.ErrNonEmptyCalldata,
		},
		{
			name: "gas limit below fee",
			tx:   newSignedEthTransfer(t, key, 0, recipient, units.Avax, 1, txs.EthRLPChainID, 0, nil),
			err:  errEthGasLimitExceeded,
		},
		{
			name: "insufficient funds",
			tx:   newSignedEthTransfer(t, key, 0, recipient, 1000*units.Avax, 10*units.Avax, txs.EthRLPChainID, 0, nil),
			err:  errEthInsufficientFunds,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := StandardTx(&env.backend, feeCalculator, tt.tx, onAcceptState)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestEthRLPTxSenderRecovery(t *testing.T) {
	require := require.New(t)

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	recipient := ids.GenerateTestShortID()

	tx := newSignedEthTransfer(t, key, 7, recipient, units.Avax, units.Avax, txs.EthRLPChainID, 0, nil)
	unsigned := tx.Unsigned.(*txs.EthRLPTx)
	require.NoError(unsigned.SyntacticVerify(nil))
	require.Equal(ids.ShortID(key.PublicKey().EthAddress()), unsigned.Sender)
	require.Equal(recipient, unsigned.Recipient)
	require.Equal(units.Avax, unsigned.AmountNAVAX)
	require.Equal(uint64(7), unsigned.Parsed.Nonce())
}
