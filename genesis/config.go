// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/formatting"
	"github.com/chain4travel/caminogo/utils/wrappers"

	safemath "github.com/chain4travel/caminogo/utils/math"
)

type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

type Allocation struct {
	ETHAddr        ids.ShortID    `json:"ethAddr"`
	AVAXAddr       ids.ShortID    `json:"avaxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

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

type Staker struct {
	NodeID        ids.ShortID `json:"nodeID"`
	RewardAddress ids.ShortID `json:"rewardAddress"`
	DelegationFee uint32      `json:"delegationFee"`
}

func (s Staker) Unparse(networkID uint32) (UnparsedStaker, error) {
	avaxAddr, err := formatting.FormatAddress(
		"X",
		constants.GetHRP(networkID),
		s.RewardAddress.Bytes(),
	)
	return UnparsedStaker{
		NodeID:        s.NodeID.PrefixedString(constants.NodeIDPrefix),
		RewardAddress: avaxAddr,
		DelegationFee: s.DelegationFee,
	}, err
}

type LockRuleOffer struct {
	InterestRate float64 `json:"interestRate"`
	Start        uint64  `json:"start"`
	End          uint64  `json:"end"`
	MinAmount    uint64  `json:"minAmount"`
	Duration     uint64  `json:"duration"`
}

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID uint32 `json:"networkID"`

	Allocations []Allocation `json:"allocations"`

	StartTime                  uint64          `json:"startTime"`
	InitialStakeDuration       uint64          `json:"initialStakeDuration"`
	InitialStakeDurationOffset uint64          `json:"initialStakeDurationOffset"`
	InitialStakedFunds         []ids.ShortID   `json:"initialStakedFunds"`
	InitialStakers             []Staker        `json:"initialStakers"`
	LockRuleOffers             []LockRuleOffer `json:"lockRuleOffers"`

	CChainGenesis string `json:"cChainGenesis"`

	Message string `json:"message"`
}

func (c Config) Unparse() (UnparsedConfig, error) {
	uc := UnparsedConfig{
		NetworkID:                  c.NetworkID,
		Allocations:                make([]UnparsedAllocation, len(c.Allocations)),
		StartTime:                  c.StartTime,
		InitialStakeDuration:       c.InitialStakeDuration,
		InitialStakeDurationOffset: c.InitialStakeDurationOffset,
		InitialStakedFunds:         make([]string, len(c.InitialStakedFunds)),
		InitialStakers:             make([]UnparsedStaker, len(c.InitialStakers)),
		CChainGenesis:              c.CChainGenesis,
		Message:                    c.Message,
		LockRuleOffers:             c.LockRuleOffers,
	}
	for i, a := range c.Allocations {
		ua, err := a.Unparse(uc.NetworkID)
		if err != nil {
			return uc, err
		}
		uc.Allocations[i] = ua
	}
	for i, isa := range c.InitialStakedFunds {
		avaxAddr, err := formatting.FormatAddress(
			"X",
			constants.GetHRP(uc.NetworkID),
			isa.Bytes(),
		)
		if err != nil {
			return uc, err
		}
		uc.InitialStakedFunds[i] = avaxAddr
	}
	for i, is := range c.InitialStakers {
		uis, err := is.Unparse(c.NetworkID)
		if err != nil {
			return uc, err
		}
		uc.InitialStakers[i] = uis
	}

	return uc, nil
}

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
	// CaminoConfig is the config that should be used to generate the camino
	// mainnet genesis.
	CaminoConfig Config

	// ColumbusConfig is the config that should be used to generate the columbus
	// genesis.
	ColumbusConfig Config

	// LocalConfig is the config that should be used to generate a local
	// genesis.
	LocalConfig Config
)

func init() {
	unparsedCaminoConfig := UnparsedConfig{}
	unparsedColumbusConfig := UnparsedConfig{}
	unparsedLocalConfig := UnparsedConfig{}

	errs := wrappers.Errs{}
	errs.Add(
		json.Unmarshal([]byte(caminoGenesisConfigJSON), &unparsedCaminoConfig),
		json.Unmarshal([]byte(columbusGenesisConfigJSON), &unparsedColumbusConfig),
		json.Unmarshal([]byte(localGenesisConfigJSON), &unparsedLocalConfig),
	)
	if errs.Errored() {
		panic(errs.Err)
	}

	caminoConfig, err := unparsedCaminoConfig.Parse()
	errs.Add(err)
	CaminoConfig = caminoConfig

	columbusConfig, err := unparsedColumbusConfig.Parse()
	errs.Add(err)
	ColumbusConfig = columbusConfig

	localConfig, err := unparsedLocalConfig.Parse()
	errs.Add(err)
	LocalConfig = localConfig

	if errs.Errored() {
		panic(errs.Err)
	}
}

func GetConfig(networkID uint32) *Config {
	switch networkID {
	case constants.CaminoID:
		return &CaminoConfig
	case constants.ColumbusID:
		return &ColumbusConfig
	case constants.LocalID:
		return &LocalConfig
	default:
		tempConfig := LocalConfig
		tempConfig.NetworkID = networkID
		return &tempConfig
	}
}

// GetConfigFile loads a *Config from a provided filepath.
func GetConfigFile(fp string) (*Config, error) {
	bytes, err := os.ReadFile(filepath.Clean(fp))
	if err != nil {
		return nil, fmt.Errorf("unable to load file %s: %w", fp, err)
	}
	return parseGenesisJSONBytesToConfig(bytes)
}

// GetConfigContent loads a *Config from a provided environment variable
func GetConfigContent(genesisContent string) (*Config, error) {
	bytes, err := base64.StdEncoding.DecodeString(genesisContent)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base64 content: %w", err)
	}
	return parseGenesisJSONBytesToConfig(bytes)
}

func parseGenesisJSONBytesToConfig(bytes []byte) (*Config, error) {
	var unparsedConfig UnparsedConfig
	if err := json.Unmarshal(bytes, &unparsedConfig); err != nil {
		return nil, fmt.Errorf("could not unmarshal JSON: %w", err)
	}

	config, err := unparsedConfig.Parse()
	if err != nil {
		return nil, fmt.Errorf("unable to parse config: %w", err)
	}
	return &config, nil
}
