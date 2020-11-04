// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package conflicts

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

// TestTx is a useful test tx
type TestTx struct {
	choices.TestDecidable

	EpochV        uint32
	RestrictionsV []ids.ID
	InputIDsV     ids.Set
	OutputIDsV    ids.Set
}

// Epoch implements the Tx interface
func (t *TestTx) Epoch() uint32 { return t.EpochV }

// Restrictions implements the Tx interface
func (t *TestTx) Restrictions() []ids.ID { return t.RestrictionsV }

// InputIDs implements the Tx interface
func (t *TestTx) InputIDs() ids.Set { return t.InputIDsV }

// OutputIDs implements the Tx interface
func (t *TestTx) OutputIDs() ids.Set { return t.OutputIDsV }
