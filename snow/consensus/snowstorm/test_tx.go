// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

// TestTx is a useful test transaction
type TestTx struct {
	Identifier ids.ID
	Deps       []Tx
	Ins        ids.Set
	Stat       choices.Status
	Validity   error
	Bits       []byte
}

// ID implements the Consumer interface
func (tx *TestTx) ID() ids.ID { return tx.Identifier }

// Dependencies implements the Consumer interface
func (tx *TestTx) Dependencies() []Tx { return tx.Deps }

// InputIDs implements the Consumer interface
func (tx *TestTx) InputIDs() ids.Set { return tx.Ins }

// Status implements the Consumer interface
func (tx *TestTx) Status() choices.Status { return tx.Stat }

// Accept implements the Consumer interface
func (tx *TestTx) Accept() error { tx.Stat = choices.Accepted; return tx.Validity }

// Reject implements the Consumer interface
func (tx *TestTx) Reject() error { tx.Stat = choices.Rejected; return tx.Validity }

// Reset sets the status to pending
func (tx *TestTx) Reset() { tx.Stat = choices.Processing }

// Verify returns nil
func (tx *TestTx) Verify() error { return tx.Validity }

// Bytes returns the bits
func (tx *TestTx) Bytes() []byte { return tx.Bits }
