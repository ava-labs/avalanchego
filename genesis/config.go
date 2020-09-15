// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	safemath "github.com/ava-labs/avalanche-go/utils/math"
)

// LockedAmount ...
type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

// Allocation ...
type Allocation struct {
	ETHAddr        ids.ShortID    `json:"ethAddr"`
	AVAXAddr       ids.ShortID    `json:"avaxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID uint32 `json:"networkID"`

	Allocations []Allocation `json:"allocations"`

	StartTime                   uint64        `json:"startTime"`
	InitialStakeDuration        uint64        `json:"initialStakeDuration"`
	InitialStakeDurationOffset  uint64        `json:"initialStakeDurationOffset"`
	InitialStakeAddresses       []ids.ShortID `json:"initialStakeAddresses"`
	InitialStakeNodeIDs         []ids.ShortID `json:"initialStakeNodeIDs"`
	InitialStakeRewardAddresses []ids.ShortID `json:"initialStakeRewardAddresses"`

	CChainGenesis string `json:"cChainGenesis"`

	Message string `json:"message"`
}

// InitialSupply ...
func (c *Config) InitialSupply() (uint64, error) {
	initialSupply := uint64(0)
	for _, allocation := range c.Allocations {
		newInitialSupply, err := safemath.Add64(initialSupply, allocation.InitialAmount)
		if err != nil {
			return 0, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			newInitialSupply, err = safemath.Add64(newInitialSupply, unlock.Amount)
			if err != nil {
				return 0, err
			}
		}
		initialSupply = newInitialSupply
	}
	return initialSupply, nil
}

var (
	// ManhattenConfig is the config that should be used to generate the
	// manhatten genesis.
	ManhattenConfig Config

	// LocalConfig is the config that should be used to generate a local
	// genesis.
	LocalConfig Config
)

func init() {
	errs := wrappers.Errs{}
	errs.Add(
		json.Unmarshal([]byte(manhattenGenesisConfigJSON), &ManhattenConfig),
		json.Unmarshal([]byte(localGenesisConfigJSON), &LocalConfig),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// GetConfig ...
func GetConfig(networkID uint32) *Config {
	switch networkID {
	case constants.ManhattenID:
		return &ManhattenConfig
	case constants.LocalID:
		return &LocalConfig
	default:
		tempConfig := LocalConfig
		tempConfig.NetworkID = networkID
		return &tempConfig
	}
}
