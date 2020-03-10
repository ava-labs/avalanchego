// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/gecko/ids"
)

// ID that this VM uses when labeled
var (
	ID = ids.NewID([32]byte{'a', 'v', 'm'})
)

// Factory ...
type Factory struct{}

// New ...
func (f *Factory) New() interface{} { return &VM{} }
