// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// ID of the platform VM
var (
	ID = ids.NewID([32]byte{'p', 'l', 'a', 't', 'f', 'o', 'r', 'm', 'v', 'm'})
)

// Factory can create new instances of the Platform Chain
type Factory struct {
	ChainManager     chains.Manager
	Validators       validators.Manager
	StakingEnabled   bool
	Fee              uint64
	MinStake         uint64
	UptimePercentage float64
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		chainManager:     f.ChainManager,
		vdrMgr:           f.Validators,
		stakingEnabled:   f.StakingEnabled,
		txFee:            f.Fee,
		minStake:         f.MinStake,
		uptimePercentage: f.UptimePercentage,
	}, nil
}
