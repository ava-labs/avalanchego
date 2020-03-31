// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
)

var (
	errNilUTXOID = errors.New("nil utxo ID is not valid")
	errNilTxID   = errors.New("nil tx ID is not valid")
)

// UTXOID ...
type UTXOID struct {
	// Serialized:
	TxID        ids.ID `serialize:"true" json:"txID"`
	OutputIndex uint32 `serialize:"true" json:"outputIndex"`

	// Symbol is false if the UTXO should be part of the DB
	Symbol bool
	// id is the unique ID of a UTXO, it is calculated from TxID and OutputIndex
	id ids.ID
}

// InputSource returns the source of the UTXO that this input is spending
func (utxo *UTXOID) InputSource() (ids.ID, uint32) { return utxo.TxID, utxo.OutputIndex }

// InputID returns a unique ID of the UTXO that this input is spending
func (utxo *UTXOID) InputID() ids.ID {
	if utxo.id.IsZero() {
		utxo.id = utxo.TxID.Prefix(uint64(utxo.OutputIndex))
	}
	return utxo.id
}

// Symbolic returns if this is the ID of a UTXO in the DB, or if it is a
// symbolic input
func (utxo *UTXOID) Symbolic() bool { return utxo.Symbol }

// Verify implements the verify.Verifiable interface
func (utxo *UTXOID) Verify() error {
	switch {
	case utxo == nil:
		return errNilUTXOID
	case utxo.TxID.IsZero():
		return errNilTxID
	default:
		return nil
	}
}
