// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"github.com/ava-labs/gecko/ids"
)

// ID this VM should be referenced with
var (
	ID = ids.NewID([32]byte{'s', 'p', 'd', 'a', 'g', 'v', 'm'})
)

// Factory ...
type Factory struct{ TxFee uint64 }

// New ...
func (f *Factory) New() (interface{}, error) {
	return &VM{TxFee: f.TxFee}, nil
}
