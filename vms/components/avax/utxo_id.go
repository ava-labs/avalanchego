// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var errNilUTXOID = errors.New("nil utxo ID is not valid")

type UTXOID struct {
	// Serialized:
	TxID        ids.ID `serialize:"true" json:"txID"`
	OutputIndex uint32 `serialize:"true" json:"outputIndex"`

	// Symbol is false if the UTXO should be part of the DB
	Symbol bool `json:"-"`
	// id is the unique ID of a UTXO, it is calculated from TxID and OutputIndex
	id ids.ID
}

// InputSource returns the source of the UTXO that this input is spending
func (utxo *UTXOID) InputSource() (ids.ID, uint32) { return utxo.TxID, utxo.OutputIndex }

// InputID returns a unique ID of the UTXO that this input is spending
func (utxo *UTXOID) InputID() ids.ID {
	if utxo.id == ids.Empty {
		utxo.id = utxo.TxID.Prefix(uint64(utxo.OutputIndex))
	}
	return utxo.id
}

// Symbolic returns if this is the ID of a UTXO in the DB, or if it is a
// symbolic input
func (utxo *UTXOID) Symbolic() bool { return utxo.Symbol }

func (utxo *UTXOID) String() string {
	return fmt.Sprintf("%s:%d", utxo.TxID, utxo.OutputIndex)
}

// Verify implements the verify.Verifiable interface
func (utxo *UTXOID) Verify() error {
	switch {
	case utxo == nil:
		return errNilUTXOID
	default:
		return nil
	}
}

type innerSortUTXOIDs []*UTXOID

func (utxos innerSortUTXOIDs) Less(i, j int) bool {
	iID, iIndex := utxos[i].InputSource()
	jID, jIndex := utxos[j].InputSource()

	switch bytes.Compare(iID[:], jID[:]) {
	case -1:
		return true
	case 0:
		return iIndex < jIndex
	default:
		return false
	}
}
func (utxos innerSortUTXOIDs) Len() int      { return len(utxos) }
func (utxos innerSortUTXOIDs) Swap(i, j int) { utxos[j], utxos[i] = utxos[i], utxos[j] }

func SortUTXOIDs(utxos []*UTXOID) { sort.Sort(innerSortUTXOIDs(utxos)) }

func IsSortedAndUniqueUTXOIDs(utxos []*UTXOID) bool {
	return utils.IsSortedAndUnique(innerSortUTXOIDs(utxos))
}
