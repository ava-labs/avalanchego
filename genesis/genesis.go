// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	xchaintxs "github.com/ava-labs/avalanchego/vms/avm/txs"
	pchaintxs "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	configChainIDAlias = "X"
)

var (
	errStakeDurationTooHigh            = errors.New("initial stake duration larger than maximum configured")
	errNoInitiallyStakedFunds          = errors.New("initial staked funds cannot be empty")
	errNoSupply                        = errors.New("initial supply must be > 0")
	errNoStakeDuration                 = errors.New("initial stake duration must be > 0")
	errNoStakers                       = errors.New("initial stakers must be > 0")
	errNoCChainGenesis                 = errors.New("C-Chain genesis cannot be empty")
	errNoTxs                           = errors.New("genesis creates no transactions")
	errNoAllocationToStake             = errors.New("no allocation to stake")
	errDuplicateInitiallyStakedAddress = errors.New("duplicate initially staked address")
	errConflictingNetworkIDs           = errors.New("conflicting networkIDs")
	errFutureStartTime                 = errors.New("startTime cannot be in the future")
	errInitialStakeDurationTooLow      = errors.New("initial stake duration is too low")
	errOverridesStandardNetworkConfig  = errors.New("overrides standard network genesis config")
	errAllocationsLockedAmountTooLow   = errors.New("total allocations locked amount is too low")
)

// validateInitialStakedFunds ensures all staked
// funds have allocations and that all staked
// funds are unique.
//
// This function assumes that NetworkID in *Config has already
// been checked for correctness.
func validateInitialStakedFunds(config *Config) error {
	if len(config.InitialStakedFunds) == 0 {
		return errNoInitiallyStakedFunds
	}

	allocationSet := set.Set[ids.ShortID]{}
	initialStakedFundsSet := set.Set[ids.ShortID]{}
	for _, allocation := range config.Allocations {
		// It is ok to have duplicates as different
		// ethAddrs could claim to the same avaxAddr.
		allocationSet.Add(allocation.AVAXAddr)
	}

	for _, staker := range config.InitialStakedFunds {
		if initialStakedFundsSet.Contains(staker) {
			avaxAddr, err := address.Format(
				configChainIDAlias,
				constants.GetHRP(config.NetworkID),
				staker.Bytes(),
			)
			if err != nil {
				return fmt.Errorf(
					"unable to format address from %s",
					staker.String(),
				)
			}

			return fmt.Errorf(
				"%w: %s",
				errDuplicateInitiallyStakedAddress,
				avaxAddr,
			)
		}
		initialStakedFundsSet.Add(staker)

		if !allocationSet.Contains(staker) {
			avaxAddr, err := address.Format(
				configChainIDAlias,
				constants.GetHRP(config.NetworkID),
				staker.Bytes(),
			)
			if err != nil {
				return fmt.Errorf(
					"unable to format address from %s",
					staker.String(),
				)
			}

			return fmt.Errorf(
				"%w in address %s",
				errNoAllocationToStake,
				avaxAddr,
			)
		}
	}

	return nil
}

// validateAllocationsLockedAmount ensures that the sum of all locked
// allocation amounts is at least the number of initial stakers.
func validateAllocationsLockedAmount(config *Config) error {
	totalLocked := uint64(0)
	for _, allocation := range config.Allocations {
		for _, unlock := range allocation.UnlockSchedule {
			totalLocked += unlock.Amount
		}
	}
	stakersCount := len(config.InitialStakers)
	if totalLocked < uint64(stakersCount) {
		return fmt.Errorf(
			"%w: %d locked < %d stakers",
			errAllocationsLockedAmountTooLow,
			totalLocked,
			stakersCount,
		)
	}
	return nil
}

// validateConfig returns an error if the provided
// *Config is not considered valid.
func validateConfig(networkID uint32, config *Config, stakingCfg *StakingConfig) error {
	if networkID != config.NetworkID {
		return fmt.Errorf(
			"%w: expected %d but config contains %d",
			errConflictingNetworkIDs,
			networkID,
			config.NetworkID,
		)
	}

	initialSupply, err := config.InitialSupply()
	switch {
	case err != nil:
		return fmt.Errorf("unable to calculate initial supply: %w", err)
	case initialSupply == 0:
		return errNoSupply
	}

	startTime := time.Unix(int64(config.StartTime), 0)
	if time.Since(startTime) < 0 {
		return fmt.Errorf(
			"%w: %s",
			errFutureStartTime,
			startTime,
		)
	}

	// We don't impose any restrictions on the minimum
	// stake duration to enable complex testing configurations
	// but recommend setting a minimum duration of at least
	// 15 minutes.
	if config.InitialStakeDuration == 0 {
		return errNoStakeDuration
	}

	// Initial stake duration of genesis validators must be
	// not larger than maximal stake duration specified for any validator.
	if config.InitialStakeDuration > uint64(stakingCfg.MaxStakeDuration.Seconds()) {
		return errStakeDurationTooHigh
	}

	if len(config.InitialStakers) == 0 {
		return errNoStakers
	}

	offsetTimeRequired := config.InitialStakeDurationOffset * uint64(len(config.InitialStakers)-1)
	if offsetTimeRequired > config.InitialStakeDuration {
		return fmt.Errorf(
			"%w must be at least %d",
			errInitialStakeDurationTooLow,
			offsetTimeRequired,
		)
	}

	if err := validateInitialStakedFunds(config); err != nil {
		return fmt.Errorf("initial staked funds validation failed: %w", err)
	}

	if err := validateAllocationsLockedAmount(config); err != nil {
		return err
	}

	if len(config.CChainGenesis) == 0 {
		return errNoCChainGenesis
	}

	return nil
}

// FromFile returns the genesis data of the Platform Chain.
//
// Since an Avalanche network has exactly one Platform Chain, and the Platform
// Chain defines the genesis state of the network (who is staking, which chains
// exist, etc.), defining the genesis state of the Platform Chain is the same as
// defining the genesis state of the network.
//
// FromFile accepts:
// 1) The ID of the new network. [networkID]
// 2) The location of a custom genesis config to load. [filepath]
//
// If [filepath] is empty or the given network ID is Mainnet, Testnet, or Local, returns error.
// If [filepath] is non-empty and networkID isn't Mainnet, Testnet, or Local,
// loads the network genesis data from the config at [filepath].
//
// FromFile returns:
//
//  1. The byte representation of the genesis state of the platform chain
//     (ie the genesis state of the network)
//  2. The asset ID of AVAX
func FromFile(networkID uint32, filepath string, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	switch networkID {
	case constants.MainnetID, constants.TestnetID, constants.LocalID:
		return nil, ids.Empty, fmt.Errorf(
			"%w: %s",
			errOverridesStandardNetworkConfig,
			constants.NetworkName(networkID),
		)
	}

	config, err := GetConfigFile(filepath)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("unable to load provided genesis config at %s: %w", filepath, err)
	}

	if err := validateConfig(networkID, config, stakingCfg); err != nil {
		return nil, ids.Empty, fmt.Errorf("genesis config validation failed: %w", err)
	}

	return FromConfig(config)
}

// FromFlag returns the genesis data of the Platform Chain.
//
// Since an Avalanche network has exactly one Platform Chain, and the Platform
// Chain defines the genesis state of the network (who is staking, which chains
// exist, etc.), defining the genesis state of the Platform Chain is the same as
// defining the genesis state of the network.
//
// FromFlag accepts:
// 1) The ID of the new network. [networkID]
// 2) The content of a custom genesis config to load. [genesisContent]
//
// If [genesisContent] is empty or the given network ID is Mainnet, Testnet, or Local, returns error.
// If [genesisContent] is non-empty and networkID isn't Mainnet, Testnet, or Local,
// loads the network genesis data from [genesisContent].
//
// FromFlag returns:
//
//  1. The byte representation of the genesis state of the platform chain
//     (ie the genesis state of the network)
//  2. The asset ID of AVAX
func FromFlag(networkID uint32, genesisContent string, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	switch networkID {
	case constants.MainnetID, constants.TestnetID, constants.LocalID:
		return nil, ids.Empty, fmt.Errorf(
			"%w: %s",
			errOverridesStandardNetworkConfig,
			constants.NetworkName(networkID),
		)
	}

	customConfig, err := GetConfigContent(genesisContent)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("unable to load genesis content from flag: %w", err)
	}

	if err := validateConfig(networkID, customConfig, stakingCfg); err != nil {
		return nil, ids.Empty, fmt.Errorf("genesis config validation failed: %w", err)
	}

	return FromConfig(customConfig)
}

// FromConfig returns:
//
//  1. The byte representation of the genesis state of the platform chain
//     (ie the genesis state of the network)
//  2. The asset ID of AVAX
func FromConfig(config *Config) ([]byte, ids.ID, error) {
	hrp := constants.GetHRP(config.NetworkID)

	// Specify the genesis state of the AVM
	avax := avm.AssetDefinition{
		Name:         "Avalanche",
		Symbol:       "AVAX",
		Denomination: 9,
		InitialState: avm.AssetInitialState{},
	}
	memoBytes := []byte{}
	xAllocations := []Allocation(nil)
	for _, allocation := range config.Allocations {
		if allocation.InitialAmount > 0 {
			xAllocations = append(xAllocations, allocation)
		}
	}
	utils.Sort(xAllocations)

	for _, allocation := range xAllocations {
		addr, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
		if err != nil {
			return nil, ids.Empty, err
		}

		avax.InitialState.FixedCap = append(avax.InitialState.FixedCap, avm.Holder{
			Amount:  allocation.InitialAmount,
			Address: addr,
		})
		memoBytes = append(memoBytes, allocation.ETHAddr.Bytes()...)
	}
	avax.Memo = memoBytes

	avmGenesis, err := avm.NewGenesis(
		config.NetworkID,
		map[string]avm.AssetDefinition{
			"AVAX": avax, // The AVM starts out with one asset: AVAX
		},
	)
	if err != nil {
		return nil, ids.Empty, err
	}
	avmGenesisBytes, err := avmGenesis.Bytes()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't serialize avm genesis: %w", err)
	}

	avaxAssetID, err := AVAXAssetID(avmGenesisBytes)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't generate AVAX asset ID: %w", err)
	}

	genesisTime := time.Unix(int64(config.StartTime), 0)
	initialSupply, err := config.InitialSupply()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't calculate the initial supply: %w", err)
	}

	initiallyStaked := set.Of(config.InitialStakedFunds...)
	skippedAllocations := []Allocation(nil)

	// Build UTXOs for the Platform Chain
	platformAllocations := []genesis.Allocation{}
	for _, allocation := range config.Allocations {
		if initiallyStaked.Contains(allocation.AVAXAddr) {
			skippedAllocations = append(skippedAllocations, allocation)
			continue
		}
		addr, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
		if err != nil {
			return nil, ids.Empty, err
		}
		for _, unlock := range allocation.UnlockSchedule {
			if unlock.Amount > 0 {
				platformAllocations = append(platformAllocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  addr,
					Message:  allocation.ETHAddr.Bytes(),
				})
			}
		}
	}

	// Build validators for the Platform Chain
	validators := []genesis.PermissionlessValidator{}
	allNodeAllocations := splitAllocations(skippedAllocations, len(config.InitialStakers))
	endStakingTime := genesisTime.Add(time.Duration(config.InitialStakeDuration) * time.Second)
	stakingOffset := time.Duration(0)
	for i, staker := range config.InitialStakers {
		nodeAllocations := allNodeAllocations[i]
		endStakingTime := endStakingTime.Add(-stakingOffset)
		stakingOffset += time.Duration(config.InitialStakeDurationOffset) * time.Second

		destAddrStr, err := address.FormatBech32(hrp, staker.RewardAddress.Bytes())
		if err != nil {
			return nil, ids.Empty, err
		}

		allocations := []genesis.Allocation(nil)
		for _, allocation := range nodeAllocations {
			addr, err := address.FormatBech32(hrp, allocation.AVAXAddr.Bytes())
			if err != nil {
				return nil, ids.Empty, err
			}
			for _, unlock := range allocation.UnlockSchedule {
				allocations = append(allocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  addr,
					Message:  allocation.ETHAddr.Bytes(),
				})
			}
		}

		validators = append(validators, genesis.PermissionlessValidator{
			Validator: genesis.Validator{
				StartTime: uint64(genesisTime.Unix()),
				EndTime:   uint64(endStakingTime.Unix()),
				NodeID:    staker.NodeID,
			},
			RewardOwner: &genesis.Owner{
				Threshold: 1,
				Addresses: []string{destAddrStr},
			},
			Staked:             allocations,
			ExactDelegationFee: staker.DelegationFee,
			Signer:             staker.Signer,
		})
	}

	// Specify the chains that exist upon this network's creation
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't encode message: %w", err)
	}
	chains := []genesis.Chain{
		{
			GenesisData: avmGenesisBytes,
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        constants.AVMID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		{
			GenesisData: []byte(config.CChainGenesis),
			SubnetID:    constants.PrimaryNetworkID,
			VMID:        constants.EVMID,
			Name:        "C-Chain",
		},
	}

	pChainGenesis, err := genesis.New(
		avaxAssetID,
		config.NetworkID,
		platformAllocations,
		validators,
		chains,
		config.StartTime,
		initialSupply,
		config.Message,
	)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}
	pChainGenesisBytes, err := pChainGenesis.Bytes()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("problem while serializing platform chain's genesis state: %w", err)
	}
	return pChainGenesisBytes, avaxAssetID, nil
}

func splitAllocations(allocations []Allocation, numSplits int) [][]Allocation {
	totalAmount := uint64(0)
	for _, allocation := range allocations {
		for _, unlock := range allocation.UnlockSchedule {
			totalAmount += unlock.Amount
		}
	}

	nodeWeight := totalAmount / uint64(numSplits)
	allNodeAllocations := make([][]Allocation, 0, numSplits)

	currentNodeAllocation := []Allocation(nil)
	currentNodeAmount := uint64(0)
	for _, allocation := range allocations {
		currentAllocation := allocation
		// Already added to the X-chain
		currentAllocation.InitialAmount = 0
		// Going to be added until the correct amount is reached
		currentAllocation.UnlockSchedule = nil

		for _, unlock := range allocation.UnlockSchedule {
			for currentNodeAmount+unlock.Amount > nodeWeight && len(allNodeAllocations) < numSplits-1 {
				amountToAdd := nodeWeight - currentNodeAmount
				currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, LockedAmount{
					Amount:   amountToAdd,
					Locktime: unlock.Locktime,
				})
				unlock.Amount -= amountToAdd

				currentNodeAllocation = append(currentNodeAllocation, currentAllocation)

				allNodeAllocations = append(allNodeAllocations, currentNodeAllocation)

				currentNodeAllocation = nil
				currentNodeAmount = 0

				currentAllocation = allocation
				// Already added to the X-chain
				currentAllocation.InitialAmount = 0
				// Going to be added until the correct amount is reached
				currentAllocation.UnlockSchedule = nil
			}

			if unlock.Amount == 0 {
				continue
			}

			currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, LockedAmount{
				Amount:   unlock.Amount,
				Locktime: unlock.Locktime,
			})
			currentNodeAmount += unlock.Amount
		}

		if len(currentAllocation.UnlockSchedule) > 0 {
			currentNodeAllocation = append(currentNodeAllocation, currentAllocation)
		}
	}

	return append(allNodeAllocations, currentNodeAllocation)
}

func VMGenesis(genesisBytes []byte, vmID ids.ID) (*pchaintxs.Tx, error) {
	genesis, err := genesis.Parse(genesisBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse genesis: %w", err)
	}
	for _, chain := range genesis.Chains {
		uChain := chain.Unsigned.(*pchaintxs.CreateChainTx)
		if uChain.VMID == vmID {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("couldn't find blockchain with VM ID %s", vmID)
}

func AVAXAssetID(avmGenesisBytes []byte) (ids.ID, error) {
	parser, err := xchaintxs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	if err != nil {
		return ids.Empty, err
	}

	genesisCodec := parser.GenesisCodec()
	genesis := avm.Genesis{}
	if _, err := genesisCodec.Unmarshal(avmGenesisBytes, &genesis); err != nil {
		return ids.Empty, err
	}

	if len(genesis.Txs) == 0 {
		return ids.Empty, errNoTxs
	}
	genesisTx := genesis.Txs[0]

	tx := xchaintxs.Tx{Unsigned: &genesisTx.CreateAssetTx}
	if err := tx.Initialize(genesisCodec); err != nil {
		return ids.Empty, err
	}
	return tx.ID(), nil
}
