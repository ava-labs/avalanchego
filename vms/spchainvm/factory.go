// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"github.com/ava-labs/gecko/ids"
)

// ID this VM should be referenced by
var (
	ID = ids.NewID([32]byte{'s', 'p', 'c', 'h', 'a', 'i', 'n', 'v', 'm'})
)

// Factory ...
type Factory struct{}

// New ...
func (f *Factory) New() (interface{}, error) { return &VM{}, nil }
