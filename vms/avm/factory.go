// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// ID that this VM uses when labeled
var (
	ID = ids.ID{'a', 'v', 'm'}
)

// Factory ...
type Factory struct {
	CreationFee  uint64
	Fee          uint64
	IndexEnabled bool
}

// New ...
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		creationTxFee: f.CreationFee,
		txFee:         f.Fee,
	}, nil
}
