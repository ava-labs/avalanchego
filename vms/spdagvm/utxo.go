// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
)

// UTXO represents an unspent transaction output
type UTXO struct {
	// The ID of the transaction that produced this UTXO
	sourceID ids.ID

	// This UTXOs index within the transaction [Tx] that created this UTXO
	// e.g. if this UTXO is the first entry in [Tx.outs] then sourceIndex is 0
	sourceIndex uint32

	// The ID of this UTXO
	id ids.ID

	// The output this UTXO wraps
	out Output

	// The binary representation of this UTXO
	bytes []byte
}

// Source returns the origin of this utxo. Specifically the txID and the
// outputIndex this utxo represents
func (u *UTXO) Source() (ids.ID, uint32) { return u.sourceID, u.sourceIndex }

// ID returns a unique identifier for this utxo
func (u *UTXO) ID() ids.ID { return u.id }

// Out returns the output this utxo wraps
func (u *UTXO) Out() Output { return u.out }

// Bytes returns a binary representation of this utxo
func (u *UTXO) Bytes() []byte { return u.bytes }

// PrefixedString converts this utxo to a string representation with a prefix
// for each newline
func (u *UTXO) PrefixedString(prefix string) string {
	return fmt.Sprintf("UTXO(\n"+
		"%s    Source ID    = %s\n"+
		"%s    Source Index = %d\n"+
		"%s    Output       = %s\n"+
		"%s)",
		prefix, u.sourceID,
		prefix, u.sourceIndex,
		prefix, u.out.PrefixedString(fmt.Sprintf("%s    ", prefix)),
		prefix,
	)
}

func (u *UTXO) String() string { return u.PrefixedString("") }
