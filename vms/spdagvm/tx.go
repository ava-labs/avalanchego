// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/math"
)

var (
	errNilTx          = errors.New("nil tx")
	errNilInput       = errors.New("nil input")
	errNilOutput      = errors.New("nil output")
	errNilSig         = errors.New("nil signature")
	errWrongNetworkID = errors.New("transaction has wrong network ID")
	errWrongChainID   = errors.New("transaction has wrong chain ID")

	errOutputsNotSorted      = errors.New("outputs not sorted")
	errInputsNotSortedUnique = errors.New("inputs not sorted and unique")
	errSigsNotSortedUnique   = errors.New("sigs not sorted and unique")
	errAddrsNotSortedUnique  = errors.New("addresses not sorted and unique")
	errTimesNotSortedUnique  = errors.New("times not sorted and unique")

	errUnknownInputType  = errors.New("unknown input type")
	errUnknownOutputType = errors.New("unknown output type")

	errInputHasNoValue   = errors.New("input has no value")
	errOutputHasNoValue  = errors.New("output has no value")
	errOutputUnspendable = errors.New("output is unspendable")
	errOutputUnoptimized = errors.New("output could be optimized")

	errInputOverflow  = errors.New("inputs overflowed uint64")
	errOutputOverflow = errors.New("outputs (plus transaction fee) overflowed uint64")

	errInvalidAmount     = errors.New("amount mismatch")
	errInsufficientFunds = errors.New("insufficient funds (includes transaction fee)")
	errTimelocked        = errors.New("time locked")
	errTypeMismatch      = errors.New("input and output types don't match")
	errSpendFailed       = errors.New("input does not satisfy output's spend requirements")

	errInvalidSigLen = errors.New("signature is not the correct length")
)

// Tx is the core operation that can be performed. The tx uses the UTXO model.
// That is, a tx's inputs will consume previous tx's outputs. A tx will be
// valid if the inputs have the authority to consume the outputs they are
// attempting to consume and the inputs consume sufficient state to produce the
// outputs.
type Tx struct {
	id        ids.ID   // ID of this transaction
	networkID uint32   // The network this transaction was issued to
	chainID   ids.ID   // ID of the chain this transaction exists on
	ins       []Input  // Inputs to this transaction
	outs      []Output // Outputs of this transaction
	bytes     []byte   // Byte representation of this transaction
}

// ID of this transaction
func (t *Tx) ID() ids.ID { return t.id }

// Ins returns the ins of this tx
func (t *Tx) Ins() []Input { return t.ins }

// Outs returns the outs of this tx
func (t *Tx) Outs() []Output { return t.outs }

// UTXOs returns the UTXOs that this transaction will produce if accepted.
func (t *Tx) UTXOs() []*UTXO {
	txID := t.ID()
	utxos := []*UTXO(nil)
	c := Codec{}
	for i, out := range t.outs {
		utxo := &UTXO{
			sourceID:    txID,
			sourceIndex: uint32(i),
			id:          txID.Prefix(uint64(i)),
			out:         out,
		}
		b, _ := c.MarshalUTXO(utxo)
		utxo.bytes = b
		utxos = append(utxos, utxo)
	}
	return utxos
}

// Bytes of this transaction
func (t *Tx) Bytes() []byte { return t.bytes }

// Verify that this transaction is well formed
func (t *Tx) Verify(ctx *snow.Context, txFee uint64) error {
	switch {
	case t == nil:
		return errNilTx
	case ctx.NetworkID != t.networkID:
		return errWrongNetworkID
	case !ctx.ChainID.Equals(t.chainID):
		return errWrongChainID
	}

	if err := t.verifyIns(); err != nil {
		return err
	}
	if err := t.verifyOuts(); err != nil {
		return err
	}
	if err := t.verifyFunds(txFee); err != nil {
		return err
	}
	if err := t.verifySigs(); err != nil {
		return err
	}
	return nil
}

// verify that inputs are well-formed
func (t *Tx) verifyIns() error {
	for _, in := range t.ins {
		if in == nil {
			return errNilInput
		}
		if err := in.Verify(); err != nil {
			return err
		}
	}
	if !isSortedAndUniqueIns(t.ins) {
		return errInputsNotSortedUnique
	}
	return nil
}

// verify outputs are well-formed
func (t *Tx) verifyOuts() error {
	for _, out := range t.outs {
		if out == nil {
			return errNilOutput
		}
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedOuts(t.outs) {
		return errOutputsNotSorted
	}
	return nil
}

// Ensure that the sum of the input amounts
// is at least:
// [the sum of the output amounts] + [txFee]
func (t *Tx) verifyFunds(txFee uint64) error {
	inFunds := uint64(0)
	for _, in := range t.ins {
		err := error(nil)

		switch i := in.(type) {
		case *InputPayment:
			inFunds, err = math.Add64(inFunds, i.amount)
		default:
			return errUnknownInputType
		}

		if err != nil {
			return errInputOverflow
		}
	}
	outFunds := uint64(0)
	for _, out := range t.outs {
		err := error(nil)

		switch o := out.(type) {
		case *OutputPayment:
			outFunds, err = math.Add64(outFunds, o.amount)
		case *OutputTakeOrLeave:
			outFunds, err = math.Add64(outFunds, o.amount)
		default:
			return errUnknownOutputType
		}

		if err != nil {
			return errOutputOverflow
		}
	}
	outFundsPlusFee, err := math.Add64(outFunds, txFee)
	if err != nil {
		return errOutputOverflow
	}
	if outFundsPlusFee > inFunds {
		return errInsufficientFunds
	}
	return nil
}

// verify the signatures
func (t *Tx) verifySigs() error {
	if !crypto.EnableCrypto {
		return nil
	}

	c := Codec{}
	txBytes, err := c.MarshalUnsignedTx(t)
	if err != nil {
		return err
	}
	txHash := hashing.ComputeHash256(txBytes)

	factory := crypto.FactorySECP256K1R{}
	for _, in := range t.ins {
		switch i := in.(type) {
		case *InputPayment:
			for _, sig := range i.sigs {
				key, err := factory.RecoverHashPublicKey(txHash, sig.sig)
				if err != nil {
					return err
				}
				sig.parsedPubKey = key.Bytes()
			}
		}
	}
	return nil
}

// PrefixedString converts this tx to a string representation with a prefix for
// each newline
func (t *Tx) PrefixedString(prefix string) string {
	s := strings.Builder{}

	nestedPrefix := fmt.Sprintf("%s    ", prefix)

	ins := t.Ins()

	s.WriteString(fmt.Sprintf("Tx(\n"+
		"%s    ID      = %s\n"+
		"%s    NumIns  = %d\n",
		prefix, t.ID(),
		prefix, len(ins)))

	inFormat := fmt.Sprintf("%%s    In[%s]: %%s\n",
		formatting.IntFormat(len(ins)-1))
	for i, in := range ins {
		s.WriteString(fmt.Sprintf(inFormat,
			prefix, i,
			in.PrefixedString(nestedPrefix),
		))
	}

	outs := t.Outs()

	s.WriteString(fmt.Sprintf("%s    NumOuts = %d\n",
		prefix, len(outs)))

	outFormat := fmt.Sprintf("%%s    Out[%s]: %%s\n",
		formatting.IntFormat(len(outs)-1))
	for i, out := range outs {
		s.WriteString(fmt.Sprintf(outFormat,
			prefix, i,
			out.PrefixedString(nestedPrefix),
		))
	}
	s.WriteString(fmt.Sprintf("%s)", prefix))

	return s.String()
}

func (t *Tx) String() string { return t.PrefixedString("") }
