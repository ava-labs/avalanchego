// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/validators"
)

// ID of the platform VM
var (
	ID = ids.NewID([32]byte{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'})
)

// Factory can create new instances of the Platform Chain
type Factory struct {
	ChainManager   chains.Manager
	Validators     validators.Manager
	StakingEnabled bool
	AVA            ids.ID
	AVM            ids.ID
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		chainManager:   f.ChainManager,
		validators:     f.Validators,
		stakingEnabled: f.StakingEnabled,
		ava:            f.AVA,
		avm:            f.AVM,
	}, nil
}
