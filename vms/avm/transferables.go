// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilTransferableOutput   = errors.New("nil transferable output is not valid")
	errNilTransferableFxOutput = errors.New("nil transferable feature extension output is not valid")

	errNilTransferableInput   = errors.New("nil transferable input is not valid")
	errNilTransferableFxInput = errors.New("nil transferable feature extension input is not valid")
)

// TransferableOutput ...
type TransferableOutput struct {
	ava.Asset `serialize:"true"`

	Out FxTransferable `serialize:"true"`
}

// Output returns the feature extension output that this Output is using.
func (out *TransferableOutput) Output() FxTransferable { return out.Out }

// Verify implements the verify.Verifiable interface
func (out *TransferableOutput) Verify() error {
	switch {
	case out == nil:
		return errNilTransferableOutput
	case out.Out == nil:
		return errNilTransferableFxOutput
	default:
		return verify.All(&out.Asset, out.Out)
	}
}

type innerSortTransferableOutputs struct {
	outs  []*TransferableOutput
	codec codec.Codec
}

func (outs *innerSortTransferableOutputs) Less(i, j int) bool {
	iOut := outs.outs[i]
	jOut := outs.outs[j]

	iAssetID := iOut.AssetID()
	jAssetID := jOut.AssetID()

	switch bytes.Compare(iAssetID.Bytes(), jAssetID.Bytes()) {
	case -1:
		return true
	case 1:
		return false
	}

	iBytes, err := outs.codec.Marshal(&iOut.Out)
	if err != nil {
		return false
	}
	jBytes, err := outs.codec.Marshal(&jOut.Out)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}
func (outs *innerSortTransferableOutputs) Len() int      { return len(outs.outs) }
func (outs *innerSortTransferableOutputs) Swap(i, j int) { o := outs.outs; o[j], o[i] = o[i], o[j] }

// SortTransferableOutputs sorts output objects
func SortTransferableOutputs(outs []*TransferableOutput, c codec.Codec) {
	sort.Sort(&innerSortTransferableOutputs{outs: outs, codec: c})
}

// IsSortedTransferableOutputs returns true if output objects are sorted
func IsSortedTransferableOutputs(outs []*TransferableOutput, c codec.Codec) bool {
	return sort.IsSorted(&innerSortTransferableOutputs{outs: outs, codec: c})
}

// TransferableInput ...
type TransferableInput struct {
	ava.UTXOID `serialize:"true"`
	ava.Asset  `serialize:"true"`

	In FxTransferable `serialize:"true"`
}

// Input returns the feature extension input that this Input is using.
func (in *TransferableInput) Input() FxTransferable { return in.In }

// Verify implements the verify.Verifiable interface
func (in *TransferableInput) Verify() error {
	switch {
	case in == nil:
		return errNilTransferableInput
	case in.In == nil:
		return errNilTransferableFxInput
	default:
		return verify.All(&in.UTXOID, &in.Asset, in.In)
	}
}

type innerSortTransferableInputs []*TransferableInput

func (ins innerSortTransferableInputs) Less(i, j int) bool {
	iID, iIndex := ins[i].InputSource()
	jID, jIndex := ins[j].InputSource()

	switch bytes.Compare(iID.Bytes(), jID.Bytes()) {
	case -1:
		return true
	case 0:
		return iIndex < jIndex
	default:
		return false
	}
}
func (ins innerSortTransferableInputs) Len() int      { return len(ins) }
func (ins innerSortTransferableInputs) Swap(i, j int) { ins[j], ins[i] = ins[i], ins[j] }

func sortTransferableInputs(ins []*TransferableInput) { sort.Sort(innerSortTransferableInputs(ins)) }
func isSortedAndUniqueTransferableInputs(ins []*TransferableInput) bool {
	return utils.IsSortedAndUnique(innerSortTransferableInputs(ins))
}
