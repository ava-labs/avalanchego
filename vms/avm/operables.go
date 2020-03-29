// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilOperableInput   = errors.New("nil operable input is not valid")
	errNilOperableFxInput = errors.New("nil operable feature extension input is not valid")
)

// OperableInput ...
type OperableInput struct {
	ava.UTXOID `serialize:"true"`

	In verify.Verifiable `serialize:"true" json:"input"`
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
