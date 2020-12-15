// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	_ Tx = &TestTx{}
)

// TestTx is a useful test tx
type TestTx struct {
	IDV           ids.ID
	StatusV       choices.Status
	EpochV        uint32
	DependenciesV []Tx
	InputIDsV     []ids.ID
	BytesV        []byte
	VerifyV       map[uint32]error
	AcceptV       map[uint32]error
	RejectV       map[uint32]error
}

// ID implements the Tx interface
func (t *TestTx) ID() ids.ID { return t.IDV }

// Status implements the Tx interface
func (t *TestTx) Status() choices.Status { return t.StatusV }

// Epoch implements the Tx interface
func (t *TestTx) Epoch() uint32 { return t.EpochV }

// Dependencies implements the Tx interface
func (t *TestTx) Dependencies() []Tx { return t.DependenciesV }

// InputIDs implements the Tx interface
func (t *TestTx) InputIDs() []ids.ID { return t.InputIDsV }

// Bytes implements the Tx interface
func (t *TestTx) Bytes() []byte { return t.BytesV }

// Verify implements the Tx interface
func (t *TestTx) Verify(epoch uint32) error {
	switch t.StatusV {
	case choices.Processing:
		return t.VerifyV[epoch]
	default:
		return fmt.Errorf("verify unexpectedly called with status %s",
			t.StatusV)
	}
}

// Accept implements the Tx interface
func (t *TestTx) Accept(epoch uint32) error {
	switch t.StatusV {
	case choices.Processing:
		t.EpochV = epoch
		t.StatusV = choices.Accepted
		return t.AcceptV[epoch]
	default:
		return fmt.Errorf("invalid state transaition from %s to %s",
			t.StatusV, choices.Accepted)
	}
}

// Reject implements the Tx interface
func (t *TestTx) Reject(epoch uint32) error {
	switch t.StatusV {
	case choices.Processing:
		t.StatusV = choices.Rejected
		return t.RejectV[epoch]
	default:
		return fmt.Errorf("invalid state transaition from %s to %s",
			t.StatusV, choices.Rejected)
	}
}
