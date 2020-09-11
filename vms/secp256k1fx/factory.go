// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// ID that this Fx uses when labeled
var (
	ID = ids.NewID([32]byte{'s', 'e', 'c', 'p', '2', '5', '6', 'k', '1', 'f', 'x'})
)

// Factory ...
type Factory struct{}

// New ...
func (f *Factory) New(*snow.Context) (interface{}, error) { return &Fx{}, nil }
