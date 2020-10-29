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

	DependenciesV []Tx
	InputIDsV     ids.Set
}

// Dependencies implements the Tx interface
func (t *TestTx) Dependencies() []Tx { return t.DependenciesV }

// InputIDs implements the Tx interface
func (t *TestTx) InputIDs() ids.Set { return t.InputIDsV }
