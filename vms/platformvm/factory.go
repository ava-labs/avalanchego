// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

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
	ChainManager       chains.Manager
	Validators         validators.Manager
	StakingEnabled     bool
	CreationFee        uint64        // Transaction fee with state creation
	Fee                uint64        // Transaction fee
	MinValidatorStake  uint64        // Min amt required to validate primary network
	MaxValidatorStake  uint64        // Max amt allowed to validate primary network
	MinDelegatorStake  uint64        // Min amt that can be delegated
	MinDelegationFee   uint32        // Min fee for delegation
	UptimePercentage   float64       // Required uptime to get a reward in [0,1]
	MinStakeDuration   time.Duration // Min time allowed for validating
	MaxStakeDuration   time.Duration // Max time allowed for validating
	StakeMintingPeriod time.Duration // Staking consumption period
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(*snow.Context) (interface{}, error) {
	return &VM{
		chainManager:       f.ChainManager,
		vdrMgr:             f.Validators,
		stakingEnabled:     f.StakingEnabled,
		creationTxFee:      f.CreationFee,
		txFee:              f.Fee,
		uptimePercentage:   f.UptimePercentage,
		minValidatorStake:  f.MinValidatorStake,
		maxValidatorStake:  f.MaxValidatorStake,
		minDelegatorStake:  f.MinDelegatorStake,
		minDelegationFee:   f.MinDelegationFee,
		minStakeDuration:   f.MinStakeDuration,
		maxStakeDuration:   f.MaxStakeDuration,
		stakeMintingPeriod: f.StakeMintingPeriod,
	}, nil
}
