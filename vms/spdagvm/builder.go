// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"
)

var (
	errInputSignerMismatch = errors.New("wrong number of signers for the inputs")
	errNilSigner           = errors.New("nil signer")
	errNilChainID          = errors.New("nil chain id")
)

// Builder defines the functionality for building payment objects.
type Builder struct {
	NetworkID uint32
	ChainID   ids.ID
}

// NewInputPayment returns a new input that consumes a UTXO.
func (b Builder) NewInputPayment(sourceID ids.ID, sourceIndex uint32, amount uint64, sigs []*Sig) Input {
	SortTxSig(sigs)

	return &InputPayment{
		sourceID:    sourceID,
		sourceIndex: sourceIndex,
		amount:      amount,
		sigs:        sigs,
	}
}

// NewOutputPayment returns a new output that generates a standard UTXO.
func (b Builder) NewOutputPayment(
	amount,
	locktime uint64,
	threshold uint32,
	addresses []ids.ShortID,
) Output {
	ids.SortShortIDs(addresses)

	return &OutputPayment{
		amount:    amount,
		locktime:  locktime,
		threshold: threshold,
		addresses: addresses,
	}
}

// NewOutputTakeOrLeave returns a new output that generates a UTXO with all the
// features of the standard UTXO, plus the ability to have a different set of
// addresses spend the asset after the specified time.
func (b Builder) NewOutputTakeOrLeave(
	amount,
	locktime1 uint64,
	threshold1 uint32,
	addresses1 []ids.ShortID,
	locktime2 uint64,
	threshold2 uint32,
	addresses2 []ids.ShortID,
) Output {
	ids.SortShortIDs(addresses1)
	ids.SortShortIDs(addresses2)

	return &OutputTakeOrLeave{
		amount:     amount,
		locktime1:  locktime1,
		threshold1: threshold1,
		addresses1: addresses1,
		locktime2:  locktime2,
		threshold2: threshold2,
		addresses2: addresses2,
	}
}

// NewSig returns a new signature object. This object will specify the address
// in the UTXO that will be used to authorize the wrapping input.
func (b Builder) NewSig(index uint32) *Sig { return &Sig{index: index} }

// NewTxFromUTXOs returns a new transaction where:
//   * One of the outputs is an Output with [amount] avax that is controlled by [toAddr].
//     * This output can't be spent until at least [locktime].
//   * If there is any "change" there is another output controlled by [changeAddr] with the change.
//   * The UTXOs consumed to make this transaction are a subset of [utxos].
//   * The keys controlling [utxos] are in [keychain]
func (b Builder) NewTxFromUTXOs(keychain *Keychain, utxos []*UTXO, amount, txFee, locktime uint64,
	threshold uint32, toAddrs []ids.ShortID, changeAddr ids.ShortID, currentTime uint64) (*Tx, error) {

	ins := []Input{}            // Consumed by this transaction
	signers := []*InputSigner{} // Each corresponds to an input consumed by this transaction

	amountPlusTxFee, err := math.Add64(amount, txFee)
	if err != nil {
		return nil, errAmountOverflow
	}

	spent := uint64(0) // The sum of the UTXOs consumed in this transaction
	for i := 0; i < len(utxos) && amountPlusTxFee > spent; i++ {
		utxo := utxos[i]
		if in, signer, err := keychain.Spend(utxo, currentTime); err == nil {
			ins = append(ins, in)
			amount := in.(*InputPayment).Amount()
			spent += amount
			signers = append(signers, signer)
		}
	}

	if spent < amountPlusTxFee {
		return nil, errInsufficientFunds
	}

	outs := []Output{ // List of outputs
		b.NewOutputPayment(amount, locktime, threshold, toAddrs), // The primary output
	}

	// If there is "change", add another output
	if spent > amountPlusTxFee {
		outs = append(outs,
			b.NewOutputPayment(spent-amountPlusTxFee, 0, 1, []ids.ShortID{changeAddr}),
		)
	}

	return b.NewTx(ins, outs, signers) // Sort, marshal, sign, build the transaction
}

// NewTx returns a new transaction where:
//   * The inputs to the Tx are [ins], sorted.
//   * The outputs of the Tx are [outs], sorted
//   * The ith signer will be used to sign the ith input. This means that len([inputs]) must be == len([signers]).
// TODO: Should the signer be part of the input
func (b Builder) NewTx(ins []Input, outs []Output, signers []*InputSigner) (*Tx, error) {
	if b.ChainID.IsZero() {
		return nil, errNilChainID
	}
	if len(ins) != len(signers) {
		return nil, errInputSignerMismatch
	}

	SortOuts(outs)
	SortIns(ins, signers)

	t := &Tx{
		networkID: b.NetworkID,
		chainID:   b.ChainID,
		ins:       ins,
		outs:      outs,
	}

	c := Codec{}
	unsignedBytes, err := c.MarshalUnsignedTx(t)
	if err != nil {
		return nil, err
	}
	unsignedHash := hashing.ComputeHash256(unsignedBytes)

	for i, rawIn := range t.ins {
		switch in := rawIn.(type) {
		case *InputPayment:
			signer := signers[i]
			if signer == nil {
				return nil, errNilSigner
			}

			for j, key := range signer.Keys {
				if !crypto.EnableCrypto {
					in.sigs[j].sig = make([]byte, crypto.SECP256K1RSigLen)
					continue
				}

				sig, err := key.SignHash(unsignedHash)
				if err != nil {
					return nil, err
				}

				in.sigs[j].sig = sig
			}
		}
	}

	bytes, err := c.MarshalTx(t)
	if err != nil {
		return nil, err
	}

	t.bytes = bytes
	t.id = ids.NewID(hashing.ComputeHash256Array(t.bytes))

	return t, nil
}
