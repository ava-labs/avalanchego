// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"encoding/json"

	"github.com/ava-labs/avalanche-go/utils/formatting"

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

// Unparse ...
func (a Allocation) Unparse(networkID uint32) (UnparsedAllocation, error) {
	ua := UnparsedAllocation{
		InitialAmount:  a.InitialAmount,
		UnlockSchedule: a.UnlockSchedule,
		ETHAddr:        "0x" + hex.EncodeToString(a.ETHAddr.Bytes()),
	}
	avaxAddr, err := formatting.FormatAddress(
		"X",
		constants.GetHRP(networkID),
		a.AVAXAddr.Bytes(),
	)
	ua.AVAXAddr = avaxAddr
	return ua, err
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

// Unparse ...
func (c Config) Unparse() (UnparsedConfig, error) {
	uc := UnparsedConfig{
		NetworkID:                   c.NetworkID,
		Allocations:                 make([]UnparsedAllocation, len(c.Allocations)),
		StartTime:                   c.StartTime,
		InitialStakeDuration:        c.InitialStakeDuration,
		InitialStakeDurationOffset:  c.InitialStakeDurationOffset,
		InitialStakeAddresses:       make([]string, len(c.InitialStakeAddresses)),
		InitialStakeNodeIDs:         make([]string, len(c.InitialStakeNodeIDs)),
		InitialStakeRewardAddresses: make([]string, len(c.InitialStakeRewardAddresses)),
		CChainGenesis:               c.CChainGenesis,
		Message:                     c.Message,
	}
	for i, a := range c.Allocations {
		ua, err := a.Unparse(uc.NetworkID)
		if err != nil {
			return uc, err
		}
		uc.Allocations[i] = ua
	}
	for i, isa := range c.InitialStakeAddresses {
		avaxAddr, err := formatting.FormatAddress(
			"X",
			constants.GetHRP(uc.NetworkID),
			isa.Bytes(),
		)
		if err != nil {
			return uc, err
		}
		uc.InitialStakeAddresses[i] = avaxAddr
	}
	for i, isnID := range c.InitialStakeNodeIDs {
		uc.InitialStakeNodeIDs[i] = isnID.PrefixedString(constants.NodeIDPrefix)
	}
	for i, isa := range c.InitialStakeRewardAddresses {
		avaxAddr, err := formatting.FormatAddress(
			"X",
			constants.GetHRP(uc.NetworkID),
			isa.Bytes(),
		)
		if err != nil {
			return uc, err
		}
		uc.InitialStakeRewardAddresses[i] = avaxAddr
	}

	return uc, nil
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
	unparsedManhattenConfig := UnparsedConfig{}
	unparsedLocalConfig := UnparsedConfig{}

	errs := wrappers.Errs{}
	errs.Add(
		json.Unmarshal([]byte(manhattenGenesisConfigJSON), &unparsedManhattenConfig),
		json.Unmarshal([]byte(localGenesisConfigJSON), &unparsedLocalConfig),
	)
	if errs.Errored() {
		panic(errs.Err)
	}

	manhattenConfig, err := unparsedManhattenConfig.Parse()
	errs.Add(err)
	ManhattenConfig = manhattenConfig

	localConfig, err := unparsedLocalConfig.Parse()
	errs.Add(err)
	LocalConfig = localConfig
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
