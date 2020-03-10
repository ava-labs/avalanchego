// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilOperableOutput   = errors.New("nil operable output is not valid")
	errNilOperableFxOutput = errors.New("nil operable feature extension output is not valid")

	errNilOperableInput   = errors.New("nil operable input is not valid")
	errNilOperableFxInput = errors.New("nil operable feature extension input is not valid")
)

// OperableOutput ...
type OperableOutput struct {
	Out verify.Verifiable `serialize:"true"`
}

// Output returns the feature extension output that this Output is using.
func (out *OperableOutput) Output() verify.Verifiable { return out.Out }

// Verify implements the verify.Verifiable interface
func (out *OperableOutput) Verify() error {
	switch {
	case out == nil:
		return errNilOperableOutput
	case out.Out == nil:
		return errNilOperableFxOutput
	default:
		return out.Out.Verify()
	}
}

type innerSortOperableOutputs struct {
	outs  []*OperableOutput
	codec codec.Codec
}

func (outs *innerSortOperableOutputs) Less(i, j int) bool {
	iOut := outs.outs[i]
	jOut := outs.outs[j]

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
func (outs *innerSortOperableOutputs) Len() int      { return len(outs.outs) }
func (outs *innerSortOperableOutputs) Swap(i, j int) { o := outs.outs; o[j], o[i] = o[i], o[j] }

func sortOperableOutputs(outs []*OperableOutput, c codec.Codec) {
	sort.Sort(&innerSortOperableOutputs{outs: outs, codec: c})
}
func isSortedOperableOutputs(outs []*OperableOutput, c codec.Codec) bool {
	return sort.IsSorted(&innerSortOperableOutputs{outs: outs, codec: c})
}

// OperableInput ...
type OperableInput struct {
	UTXOID `serialize:"true"`

	In verify.Verifiable `serialize:"true"`
}

// Input returns the feature extension input that this Input is using.
func (in *OperableInput) Input() verify.Verifiable { return in.In }

// Verify implements the verify.Verifiable interface
func (in *OperableInput) Verify() error {
	switch {
	case in == nil:
		return errNilOperableInput
	case in.In == nil:
		return errNilOperableFxInput
	default:
		return verify.All(&in.UTXOID, in.In)
	}
}

type innerSortOperableInputs []*OperableInput

func (ins innerSortOperableInputs) Less(i, j int) bool {
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
func (ins innerSortOperableInputs) Len() int      { return len(ins) }
func (ins innerSortOperableInputs) Swap(i, j int) { ins[j], ins[i] = ins[i], ins[j] }

func sortOperableInputs(ins []*OperableInput) { sort.Sort(innerSortOperableInputs(ins)) }
func isSortedAndUniqueOperableInputs(ins []*OperableInput) bool {
	return utils.IsSortedAndUnique(innerSortOperableInputs(ins))
}
