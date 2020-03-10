// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
)

// Input describes the interface all inputs must implement
type Input interface {
	formatting.PrefixedStringer

	InputSource() (ids.ID, uint32)
	InputID() ids.ID

	Verify() error
}

// InputPayment is an input that consumes an output
// InputPayment implements Input
type InputPayment struct {
	// The ID of the transaction that produced the UTXO this input consumes
	sourceID ids.ID

	// The index within that transaction of the UTXO this input consumes
	sourceIndex uint32

	// The ID of the UTXO this input consumes
	utxoID ids.ID

	// The amount of the UTXO this input consumes
	amount uint64

	// The signatures used to spend the UTXOs this input consumes
	sigs []*Sig
}

// InputSource returns:
// 1) The ID of the transaction that produced the UTXO this input consumes
// 2) The index within that transaction of the UTXO this input consumes
func (in *InputPayment) InputSource() (ids.ID, uint32) { return in.sourceID, in.sourceIndex }

// InputID returns the ID of the UTXO this input consumes
func (in *InputPayment) InputID() ids.ID {
	if in.utxoID.IsZero() {
		in.utxoID = in.sourceID.Prefix(uint64(in.sourceIndex))
	}
	return in.utxoID
}

// Amount this input will produce for the tx
func (in *InputPayment) Amount() uint64 { return in.amount }

// Verify this input is syntactically valid
func (in *InputPayment) Verify() error {
	switch {
	case in == nil:
		return errNilInput
	case in.amount == 0:
		return errInputHasNoValue
	}
	// Verify the signatures are well-formed
	for _, sig := range in.sigs {
		switch {
		case sig == nil:
			return errNilSig
		case len(sig.sig) != crypto.SECP256K1RSigLen:
			return errInvalidSigLen
		}
	}
	// Verify in.sigs are sorted and unique
	if !isSortedAndUniqueTxSig(in.sigs) {
		return errSigsNotSortedUnique
	}
	return nil
}

// PrefixedString converts this input to a string representation with a prefix
// for each newline
func (in *InputPayment) PrefixedString(prefix string) string {
	s := strings.Builder{}

	s.WriteString(fmt.Sprintf("InputPayment(\n"+
		"%s    Source ID    = %s\n"+
		"%s    Source Index = %d\n"+
		"%s    Amount       = %d\n"+
		"%s    NumSigs      = %d\n",
		prefix, in.sourceID,
		prefix, in.sourceIndex,
		prefix, in.amount,
		prefix, len(in.sigs)))

	sigFormat := fmt.Sprintf("%%s    Sig[%s]: Index = %%d, Signature = %%s\n",
		formatting.IntFormat(len(in.sigs)-1))
	for i, sig := range in.sigs {
		s.WriteString(fmt.Sprintf(sigFormat,
			prefix, i, sig.index, formatting.CB58{Bytes: sig.sig},
		))
	}

	s.WriteString(fmt.Sprintf("%s)", prefix))

	return s.String()
}

func (in *InputPayment) String() string { return in.PrefixedString("") }

type sortInsData struct {
	ins     []Input
	signers []*InputSigner
}

func (ins sortInsData) Less(i, j int) bool {
	iID, iIndex := ins.ins[i].InputSource()
	jID, jIndex := ins.ins[j].InputSource()

	switch bytes.Compare(iID.Bytes(), jID.Bytes()) {
	case -1:
		return true
	case 0:
		return iIndex < jIndex
	default:
		return false
	}
}

func (ins sortInsData) Len() int { return len(ins.ins) }

func (ins sortInsData) Swap(i, j int) {
	ins.ins[j], ins.ins[i] = ins.ins[i], ins.ins[j]
	if ins.signers != nil {
		ins.signers[j], ins.signers[i] = ins.signers[i], ins.signers[j]
	}
}

// SortIns sorts the tx input list by inputID | inputIndex
func SortIns(ins []Input, signers []*InputSigner) { sort.Sort(sortInsData{ins: ins, signers: signers}) }

func isSortedAndUniqueIns(ins []Input) bool { return utils.IsSortedAndUnique(sortInsData{ins: ins}) }
