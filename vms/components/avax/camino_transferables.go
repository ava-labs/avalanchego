// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"sort"
)

type innerSortTransferableInputs struct {
	ins []*TransferableInput
}

func (ins *innerSortTransferableInputs) Less(i, j int) bool {
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

func (ins *innerSortTransferableInputs) Len() int {
	return len(ins.ins)
}

func (ins *innerSortTransferableInputs) Swap(i, j int) {
	ins.ins[j], ins.ins[i] = ins.ins[i], ins.ins[j]
}

// SortTransferableInputsWithSigners sorts the inputs and signers based on the
// input's utxo ID
func SortTransferableInputs(ins []*TransferableInput) {
	sort.Sort(&innerSortTransferableInputs{ins: ins})
}
