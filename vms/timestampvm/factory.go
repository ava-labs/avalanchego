// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timestampvm

import "github.com/ava-labs/gecko/ids"

// ID is a unique identifier for this VM
var (
	ID = ids.NewID([32]byte{'t', 'i', 'm', 'e', 's', 't', 'a', 'm', 'p'})
)

// Factory ...
type Factory struct{}

// New ...
func (f *Factory) New() (interface{}, error) { return &VM{}, nil }
