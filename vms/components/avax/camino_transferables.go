// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
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

// SortTransferableInputs sorts the inputs based on the utxoid
func SortTransferableInputs(ins []*TransferableInput) {
	sort.Sort(&innerSortTransferableInputs{ins: ins})
}

type innerSortTransferableUTXOs struct {
	utxos []*UTXO
}

func (utxos *innerSortTransferableUTXOs) Less(i, j int) bool {
	iID, iIndex := utxos.utxos[i].InputSource()
	jID, jIndex := utxos.utxos[j].InputSource()

	switch bytes.Compare(iID[:], jID[:]) {
	case -1:
		return true
	case 0:
		return iIndex < jIndex
	default:
		return false
	}
}

func (utxos *innerSortTransferableUTXOs) Len() int {
	return len(utxos.utxos)
}

func (utxos *innerSortTransferableUTXOs) Swap(i, j int) {
	utxos.utxos[j], utxos.utxos[i] = utxos.utxos[i], utxos.utxos[j]
}

// SortTransferableUTXOs sorts the utxos based on the utxoid
func SortTransferableUTXOs(utxos []*UTXO) {
	sort.Sort(&innerSortTransferableUTXOs{utxos: utxos})
}
