// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// ID that this VM uses when labeled
var (
	ID = ids.NewID([32]byte{'a', 'v', 'm'})
)

// Factory ...
type Factory struct {
	AVA      ids.ID
	Fee      uint64
	Platform ids.ID
}

// New ...
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		ava:      f.AVA,
		platform: f.Platform,
		txFee:    f.Fee,
	}, nil
}
