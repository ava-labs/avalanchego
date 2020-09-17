// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"errors"

	"github.com/ava-labs/avalanche-go/utils/constants"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// UnparsedAllocation ...
type UnparsedAllocation struct {
	ETHAddr        string         `json:"ethAddr"`
	AVAXAddr       string         `json:"avaxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

// Parse ...
func (ua UnparsedAllocation) Parse() (Allocation, error) {
	a := Allocation{
		InitialAmount:  ua.InitialAmount,
		UnlockSchedule: ua.UnlockSchedule,
	}

	if len(ua.ETHAddr) < 2 {
		return a, errors.New("invalid eth address")
	}

	ethAddrBytes, err := hex.DecodeString(ua.ETHAddr[2:])
	if err != nil {
		return a, err
	}
	ethAddr, err := ids.ToShortID(ethAddrBytes)
	if err != nil {
		return a, err
	}
	a.ETHAddr = ethAddr

	_, _, avaxAddrBytes, err := formatting.ParseAddress(ua.AVAXAddr)
	if err != nil {
		return a, err
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return a, err
	}
	a.AVAXAddr = avaxAddr

	return a, nil
}

// UnparsedConfig contains the genesis addresses used to construct a genesis
type UnparsedConfig struct {
	NetworkID uint32 `json:"networkID"`

	Allocations []UnparsedAllocation `json:"allocations"`

	StartTime                   uint64   `json:"startTime"`
	InitialStakeDuration        uint64   `json:"initialStakeDuration"`
	InitialStakeDurationOffset  uint64   `json:"initialStakeDurationOffset"`
	InitialStakeAddresses       []string `json:"initialStakeAddresses"`
	InitialStakeNodeIDs         []string `json:"initialStakeNodeIDs"`
	InitialStakeRewardAddresses []string `json:"initialStakeRewardAddresses"`

	CChainGenesis string `json:"cChainGenesis"`

	Message string `json:"message"`
}

// Parse ...
func (uc UnparsedConfig) Parse() (Config, error) {
	c := Config{
		NetworkID:                   uc.NetworkID,
		Allocations:                 make([]Allocation, len(uc.Allocations)),
		StartTime:                   uc.StartTime,
		InitialStakeDuration:        uc.InitialStakeDuration,
		InitialStakeDurationOffset:  uc.InitialStakeDurationOffset,
		InitialStakeAddresses:       make([]ids.ShortID, len(uc.InitialStakeAddresses)),
		InitialStakeNodeIDs:         make([]ids.ShortID, len(uc.InitialStakeNodeIDs)),
		InitialStakeRewardAddresses: make([]ids.ShortID, len(uc.InitialStakeRewardAddresses)),
		CChainGenesis:               uc.CChainGenesis,
		Message:                     uc.Message,
	}
	for i, ua := range uc.Allocations {
		a, err := ua.Parse()
		if err != nil {
			return c, err
		}
		c.Allocations[i] = a
	}
	for i, isa := range uc.InitialStakeAddresses {
		_, _, avaxAddrBytes, err := formatting.ParseAddress(isa)
		if err != nil {
			return c, err
		}
		avaxAddr, err := ids.ToShortID(avaxAddrBytes)
		if err != nil {
			return c, err
		}
		c.InitialStakeAddresses[i] = avaxAddr
	}
	for i, isnID := range uc.InitialStakeNodeIDs {
		nodeID, err := ids.ShortFromPrefixedString(isnID, constants.NodeIDPrefix)
		if err != nil {
			return c, err
		}
		c.InitialStakeNodeIDs[i] = nodeID
	}
	for i, isra := range uc.InitialStakeRewardAddresses {
		_, _, avaxAddrBytes, err := formatting.ParseAddress(isra)
		if err != nil {
			return c, err
		}
		avaxAddr, err := ids.ToShortID(avaxAddrBytes)
		if err != nil {
			return c, err
		}
		c.InitialStakeRewardAddresses[i] = avaxAddr
	}
	return c, nil
}
