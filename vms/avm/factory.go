// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
)

// ID that this VM uses when labeled
var (
	ID = ids.NewID([32]byte{'a', 'v', 'm'})
)

// Factory ...
type Factory struct {
	ChainManager chains.Manager
	AVA          ids.ID
	Fee          uint64
	ValidChains  ids.Set
}

// New ...
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		chainManager: f.ChainManager,
		ava:          f.AVA,
		txFee:        f.Fee,
		validChains:  f.ValidChains,
	}, nil
}
