// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	ErrNilTransferableOutput   = errors.New("nil transferable output is not valid")
	ErrNilTransferableFxOutput = errors.New("nil transferable feature extension output is not valid")
	ErrOutputsNotSorted        = errors.New("outputs not sorted")

	ErrNilTransferableInput   = errors.New("nil transferable input is not valid")
	ErrNilTransferableFxInput = errors.New("nil transferable feature extension input is not valid")
	ErrInputsNotSortedUnique  = errors.New("inputs not sorted and unique")

	_ verify.Verifiable                  = (*TransferableOutput)(nil)
	_ verify.Verifiable                  = (*TransferableInput)(nil)
	_ utils.Sortable[*TransferableInput] = (*TransferableInput)(nil)
)

// Amounter is a data structure that has an amount of something associated with it
type Amounter interface {
	snow.ContextInitializable
	// Amount returns how much value this element represents of the asset in its
	// transaction.
	Amount() uint64
}

// Coster is a data structure that has a cost associated with it
type Coster interface {
	// Cost returns how much this element costs to be included in its
	// transaction.
	Cost() (uint64, error)
}

// TransferableIn is the interface a feature extension must provide to transfer
// value between features extensions.
type TransferableIn interface {
	verify.Verifiable
	Amounter
	Coster
}

// TransferableOut is the interface a feature extension must provide to transfer
// value between features extensions.
type TransferableOut interface {
	snow.ContextInitializable
	verify.State
	Amounter
}

type TransferableOutput struct {
	Asset `serialize:"true"`
	// FxID has serialize false because we don't want this to be encoded in bytes
	FxID ids.ID          `serialize:"false" json:"fxID"`
	Out  TransferableOut `serialize:"true"  json:"output"`
}

func (out *TransferableOutput) InitCtx(ctx *snow.Context) {
	out.Out.InitCtx(ctx)
}

// Output returns the feature extension output that this Output is using.
func (out *TransferableOutput) Output() TransferableOut {
	return out.Out
}

func (out *TransferableOutput) Verify() error {
	switch {
	case out == nil:
		return ErrNilTransferableOutput
	case out.Out == nil:
		return ErrNilTransferableFxOutput
	default:
		return verify.All(&out.Asset, out.Out)
	}
}

type innerSortTransferableOutputs struct {
	outs  []*TransferableOutput
	codec codec.Manager
}

func (outs *innerSortTransferableOutputs) Less(i, j int) bool {
	iOut := outs.outs[i]
	jOut := outs.outs[j]

	iAssetID := iOut.AssetID()
	jAssetID := jOut.AssetID()

	switch bytes.Compare(iAssetID[:], jAssetID[:]) {
	case -1:
		return true
	case 1:
		return false
	}

	iBytes, err := outs.codec.Marshal(codecVersion, &iOut.Out)
	if err != nil {
		return false
	}
	jBytes, err := outs.codec.Marshal(codecVersion, &jOut.Out)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (outs *innerSortTransferableOutputs) Len() int {
	return len(outs.outs)
}

func (outs *innerSortTransferableOutputs) Swap(i, j int) {
	o := outs.outs
	o[j], o[i] = o[i], o[j]
}

// SortTransferableOutputs sorts output objects
func SortTransferableOutputs(outs []*TransferableOutput, c codec.Manager) {
	sort.Sort(&innerSortTransferableOutputs{outs: outs, codec: c})
}

// IsSortedTransferableOutputs returns true if output objects are sorted
func IsSortedTransferableOutputs(outs []*TransferableOutput, c codec.Manager) bool {
	return sort.IsSorted(&innerSortTransferableOutputs{outs: outs, codec: c})
}

type TransferableInput struct {
	UTXOID `serialize:"true"`
	Asset  `serialize:"true"`
	// FxID has serialize false because we don't want this to be encoded in bytes
	FxID ids.ID         `serialize:"false" json:"fxID"`
	In   TransferableIn `serialize:"true"  json:"input"`
}

// Input returns the feature extension input that this Input is using.
func (in *TransferableInput) Input() TransferableIn {
	return in.In
}

func (in *TransferableInput) Verify() error {
	switch {
	case in == nil:
		return ErrNilTransferableInput
	case in.In == nil:
		return ErrNilTransferableFxInput
	default:
		return verify.All(&in.UTXOID, &in.Asset, in.In)
	}
}

func (in *TransferableInput) Compare(other *TransferableInput) int {
	return in.UTXOID.Compare(&other.UTXOID)
}

type innerSortTransferableInputsWithSigners struct {
	ins     []*TransferableInput
	signers [][]*secp256k1.PrivateKey
}

func (ins *innerSortTransferableInputsWithSigners) Less(i, j int) bool {
	iID, iIndex := ins.ins[i].InputSource()
	jID, jIndex := ins.ins[j].InputSource()

	switch bytes.Compare(iID[:], jID[:]) {
	case -1:
		return true
	case 0:
		return iIndex < jIndex
	default:
		return false
	}
}

func (ins *innerSortTransferableInputsWithSigners) Len() int {
	return len(ins.ins)
}

func (ins *innerSortTransferableInputsWithSigners) Swap(i, j int) {
	ins.ins[j], ins.ins[i] = ins.ins[i], ins.ins[j]
	ins.signers[j], ins.signers[i] = ins.signers[i], ins.signers[j]
}

// SortTransferableInputsWithSigners sorts the inputs and signers based on the
// input's utxo ID
func SortTransferableInputsWithSigners(ins []*TransferableInput, signers [][]*secp256k1.PrivateKey) {
	sort.Sort(&innerSortTransferableInputsWithSigners{ins: ins, signers: signers})
}

// VerifyTx verifies that the inputs and outputs flowcheck, including a fee.
// Additionally, this verifies that the inputs and outputs are sorted.
func VerifyTx(
	feeAmount uint64,
	feeAssetID ids.ID,
	allIns [][]*TransferableInput,
	allOuts [][]*TransferableOutput,
	c codec.Manager,
) error {
	fc := NewFlowChecker()

	fc.Produce(feeAssetID, feeAmount) // The txFee must be burned

	// Add all the outputs to the flow checker and make sure they are sorted
	for _, outs := range allOuts {
		for _, out := range outs {
			if err := out.Verify(); err != nil {
				return err
			}
			fc.Produce(out.AssetID(), out.Output().Amount())
		}
		if !IsSortedTransferableOutputs(outs, c) {
			return ErrOutputsNotSorted
		}
	}

	// Add all the inputs to the flow checker and make sure they are sorted
	for _, ins := range allIns {
		for _, in := range ins {
			if err := in.Verify(); err != nil {
				return err
			}
			fc.Consume(in.AssetID(), in.Input().Amount())
		}
		if !utils.IsSortedAndUnique(ins) {
			return ErrInputsNotSortedUnique
		}
	}

	return fc.Verify()
}
