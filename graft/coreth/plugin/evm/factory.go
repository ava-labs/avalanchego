// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanche-go/ids"
)

// ID this VM should be referenced by
var (
	ID = ids.NewID([32]byte{'e', 'v', 'm'})
)

// Factory ...
type Factory struct {
	Fee uint64
}

// New ...
func (f *Factory) New() interface{} {
	return &VM{
		txFee: f.Fee,
	}
}
