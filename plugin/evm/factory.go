// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/gecko/ids"
)

// ID this VM should be referenced by
var (
	ID = ids.NewID([32]byte{'e', 'v', 'm'})
)

// Factory ...
type Factory struct {
	AVAX ids.ID
	AVM  ids.ID
	Fee  uint64
}

// New ...
func (f *Factory) New() interface{} {
	return &VM{
		avaxAssetID: f.AVAX,
		avm:         f.AVM,
		txFee:       f.Fee,
	}
}
