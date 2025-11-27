// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"errors"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

const (
	defaultGasLimit = uint64(100_000_000) // Gas limit is arbitrary

	// Arbitrarily large amount of AVAX to fund keys on the X-Chain for testing
	defaultFundedKeyXChainAmount = 30 * units.MegaAvax
)

var (
	// Arbitrarily large amount of AVAX (10^12) to fund keys on the C-Chain for testing
	defaultFundedKeyCChainAmount = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)

	errNoKeysForGenesis           = errors.New("no keys to fund for genesis")
	errInvalidNetworkIDForGenesis = errors.New("network ID can't be mainnet, testnet or local network ID for genesis")
	errMissingStakersForGenesis   = errors.New("no stakers provided for genesis")
)

// Helper type to simplify configuring X-Chain genesis balances
type XChainBalanceMap map[ids.ShortID]uint64

// Create a genesis struct valid for bootstrapping a test
// network. Note that many of the genesis fields (e.g. reward
// addresses) are randomly generated or hard-coded.
func NewTestGenesis(
	networkID uint32,
	nodes []*Node,
	keysToFund []*secp256k1.PrivateKey,
) (*genesis.UnparsedConfig, error) {
	// Validate inputs
	switch networkID {
	case constants.TestnetID, constants.MainnetID, constants.LocalID:
		return nil, stacktrace.Wrap(errInvalidNetworkIDForGenesis)
	}
	if len(nodes) == 0 {
		return nil, stacktrace.Wrap(errMissingStakersForGenesis)
	}
	if len(keysToFund) == 0 {
		return nil, stacktrace.Wrap(errNoKeysForGenesis)
	}

	initialStakers, err := stakersForNodes(networkID, nodes)
	if err != nil {
		return nil, stacktrace.Errorf("failed to configure stakers for nodes: %w", err)
	}

	// Address that controls stake doesn't matter -- generate it randomly
	stakeAddress, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		ids.GenerateTestShortID().Bytes(),
	)
	if err != nil {
		return nil, stacktrace.Errorf("failed to format stake address: %w", err)
	}

	// Ensure the total stake allows a MegaAvax per staker
	totalStake := uint64(len(initialStakers)) * units.MegaAvax

	// The eth address is only needed to link pre-mainnet assets. Until that capability
	// becomes necessary for testing, use a bogus address.
	//
	// Reference: https://github.com/ava-labs/avalanchego/issues/1365#issuecomment-1511508767
	ethAddress := "0x0000000000000000000000000000000000000000"

	now := time.Now()

	config := &genesis.UnparsedConfig{
		NetworkID: networkID,
		Allocations: []genesis.UnparsedAllocation{
			{
				ETHAddr:       ethAddress,
				AVAXAddr:      stakeAddress,
				InitialAmount: 0,
				UnlockSchedule: []genesis.LockedAmount{ // Provides stake to validators
					{
						Amount:   totalStake,
						Locktime: uint64(now.Add(7 * 24 * time.Hour).Unix()), // 1 Week
					},
				},
			},
		},
		StartTime:                  uint64(now.Unix()),
		InitialStakedFunds:         []string{stakeAddress},
		InitialStakeDuration:       365 * 24 * 60 * 60, // 1 year
		InitialStakeDurationOffset: 90 * 60,            // 90 minutes
		Message:                    "hello avalanche!",
		InitialStakers:             initialStakers,
	}

	// Ensure pre-funded keys have arbitrary large balances on both chains to support testing
	xChainBalances := make(XChainBalanceMap, len(keysToFund))
	cChainBalances := make(core.GenesisAlloc, len(keysToFund))
	for _, key := range keysToFund {
		xChainBalances[key.Address()] = defaultFundedKeyXChainAmount
		cChainBalances[key.EthAddress()] = core.GenesisAccount{
			Balance: defaultFundedKeyCChainAmount,
		}
	}

	// Set X-Chain balances
	for xChainAddress, balance := range xChainBalances {
		avaxAddr, err := address.Format("X", constants.GetHRP(networkID), xChainAddress[:])
		if err != nil {
			return nil, stacktrace.Errorf("failed to format X-Chain address: %w", err)
		}
		config.Allocations = append(
			config.Allocations,
			genesis.UnparsedAllocation{
				ETHAddr:       ethAddress,
				AVAXAddr:      avaxAddr,
				InitialAmount: balance,
				UnlockSchedule: []genesis.LockedAmount{
					{
						Amount: 20 * units.MegaAvax,
					},
					{
						Amount:   totalStake,
						Locktime: uint64(now.Add(7 * 24 * time.Hour).Unix()), // 1 Week
					},
				},
			},
		)
	}

	chainID := big.NewInt(int64(networkID))
	// Define C-Chain genesis
	cChainGenesis := &core.Genesis{
		Config:     &params.ChainConfig{ChainID: chainID},      // The rest of the config is set in coreth on VM initialization
		Difficulty: big.NewInt(0),                              // Difficulty is a mandatory field
		Timestamp:  uint64(upgrade.InitiallyActiveTime.Unix()), // This time enables Avalanche upgrades by default
		GasLimit:   defaultGasLimit,
		Alloc:      cChainBalances,
	}
	cChainGenesisBytes, err := json.Marshal(cChainGenesis)
	if err != nil {
		return nil, stacktrace.Errorf("failed to marshal C-Chain genesis: %w", err)
	}
	config.CChainGenesis = string(cChainGenesisBytes)

	return config, nil
}

// Returns staker configuration for the given set of nodes.
func stakersForNodes(networkID uint32, nodes []*Node) ([]genesis.UnparsedStaker, error) {
	// Give staking rewards for initial validators to a random address. Any testing of staking rewards
	// will be easier to perform with nodes other than the initial validators since the timing of
	// staking can be more easily controlled.
	rewardAddr, err := address.Format("X", constants.GetHRP(networkID), ids.GenerateTestShortID().Bytes())
	if err != nil {
		return nil, stacktrace.Errorf("failed to format reward address: %w", err)
	}

	// Configure provided nodes as initial stakers
	initialStakers := make([]genesis.UnparsedStaker, len(nodes))
	for i, node := range nodes {
		pop, err := node.GetProofOfPossession()
		if err != nil {
			return nil, stacktrace.Errorf("failed to derive proof of possession for node %s: %w", node.NodeID, err)
		}
		initialStakers[i] = genesis.UnparsedStaker{
			NodeID:        node.NodeID,
			RewardAddress: rewardAddr,
			DelegationFee: .01 * reward.PercentDenominator,
			Signer:        pop,
		}
	}

	return initialStakers, nil
}

// NewRandomizedTestGenesis creates a test genesis with randomized parameters
// using the ANTITHESIS_RANDOM_SEED environment variable for consistent randomization
// across all containers in antithesis tests.
func NewRandomizedTestGenesis(
	networkID uint32,
	nodes []*Node,
	keysToFund []*secp256k1.PrivateKey,
) (*genesis.UnparsedConfig, error) {
	// Get the base genesis config first
	config, err := NewTestGenesis(networkID, nodes, keysToFund)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	// Check for antithesis random seed
	antithesisSeed := os.Getenv("ANTITHESIS_RANDOM_SEED")
	if antithesisSeed == "" {
		// No randomization requested, return the original config
		return config, nil
	}

	// Parse the seed and create a deterministic random source
	seed, err := strconv.ParseInt(antithesisSeed, 10, 64)
	if err != nil {
		return nil, stacktrace.Errorf("failed to parse ANTITHESIS_RANDOM_SEED: %w", err)
	}

	// Create a deterministic random source
	rng := rand.New(rand.NewSource(seed)) // #nosec G404

	// Randomize the genesis parameters
	if err := randomizeGenesisParams(rng, config); err != nil {
		return nil, stacktrace.Errorf("failed to randomize genesis params: %w", err)
	}

	return config, nil
}

// randomizeGenesisParams randomizes various genesis parameters
func randomizeGenesisParams(rng *rand.Rand, config *genesis.UnparsedConfig) error {
	// Parse C-Chain genesis to modify it
	var cChainGenesis core.Genesis
	if err := json.Unmarshal([]byte(config.CChainGenesis), &cChainGenesis); err != nil {
		return stacktrace.Errorf("failed to unmarshal C-Chain genesis: %w", err)
	}

	// Randomize gas limit (between 50M and 200M)
	cChainGenesis.GasLimit = uint64(50_000_000 + rng.Intn(150_000_000))

	// Randomize initial stake duration (between 12 hours and 48 hours)
	minStakeDuration := 12 * 60 * 60 // 12 hours in seconds
	maxStakeDuration := 48 * 60 * 60 // 48 hours in seconds
	stakeDurationRange := maxStakeDuration - minStakeDuration
	config.InitialStakeDuration = uint64(minStakeDuration + rng.Intn(stakeDurationRange))

	// Randomize initial stake duration offset (between 30 minutes and 3 hours)
	minOffset := 30 * 60     // 30 minutes in seconds
	maxOffset := 3 * 60 * 60 // 3 hours in seconds
	offsetRange := maxOffset - minOffset
	config.InitialStakeDurationOffset = uint64(minOffset + rng.Intn(offsetRange))

	// Serialize the modified C-Chain genesis back
	cChainGenesisBytes, err := json.Marshal(&cChainGenesis)
	if err != nil {
		return stacktrace.Errorf("failed to marshal C-Chain genesis: %w", err)
	}
	config.CChainGenesis = string(cChainGenesisBytes)

	return nil
}

// RandomizedParams creates randomized network parameters for testing
// using the ANTITHESIS_RANDOM_SEED environment variable.
func RandomizedParams(rng *rand.Rand, baseParams genesis.Params) genesis.Params {
	// Create a copy of the base params
	params := baseParams

	// Randomize P-Chain minimum gas price
	// Range: 1 to 1000 nAVAX (1 to 1000 * 10^9 wei equivalent)
	minPrice := 1 + rng.Intn(1000)
	params.TxFeeConfig.DynamicFeeConfig.MinPrice = gas.Price(uint64(minPrice) * units.NanoAvax)

	// Randomize validator fee minimum price
	// Range: 1 to 1000 nAVAX
	validatorMinPrice := 1 + rng.Intn(1000)
	params.TxFeeConfig.ValidatorFeeConfig.MinPrice = gas.Price(uint64(validatorMinPrice) * units.NanoAvax)

	// Randomize transaction fees
	// Base transaction fee: 0.1 to 10 milliAVAX
	baseFeeMultiplier := 1 + rng.Intn(100) // 1 to 100
	params.TxFeeConfig.TxFee = uint64(baseFeeMultiplier) * (units.MilliAvax / 10)

	// Create asset transaction fee: 0.5 to 50 milliAVAX
	createAssetFeeMultiplier := 5 + rng.Intn(500) // 5 to 500 (0.5 to 50 milliAVAX)
	params.TxFeeConfig.CreateAssetTxFee = uint64(createAssetFeeMultiplier) * (units.MilliAvax / 10)

	// Randomize gas capacity and throughput parameters
	// Max capacity: 500K to 2M
	params.TxFeeConfig.DynamicFeeConfig.MaxCapacity = gas.Gas(500_000 + rng.Intn(1_500_000))

	// Max per second: 50K to 200K
	maxPerSecond := 50_000 + rng.Intn(150_000)
	params.TxFeeConfig.DynamicFeeConfig.MaxPerSecond = gas.Gas(maxPerSecond)

	// Target per second: 25% to 75% of max per second
	targetRatio := 25 + rng.Intn(51) // 25 to 75
	params.TxFeeConfig.DynamicFeeConfig.TargetPerSecond = gas.Gas(maxPerSecond * targetRatio / 100)

	// Randomize validator fee capacity and target
	validatorCapacity := 10_000 + rng.Intn(40_000) // 10K to 50K
	params.TxFeeConfig.ValidatorFeeConfig.Capacity = gas.Gas(validatorCapacity)

	// Target: 25% to 75% of capacity
	validatorTargetRatio := 25 + rng.Intn(51) // 25 to 75
	params.TxFeeConfig.ValidatorFeeConfig.Target = gas.Gas(validatorCapacity * validatorTargetRatio / 100)

	// Randomize staking parameters
	// Min validator stake: 1 to 5 KiloAVAX
	minValidatorStakeMultiplier := 1 + rng.Intn(5)
	params.StakingConfig.MinValidatorStake = uint64(minValidatorStakeMultiplier) * units.KiloAvax

	// Max validator stake: 2 to 10 MegaAVAX
	maxValidatorStakeMultiplier := 2 + rng.Intn(9)
	params.StakingConfig.MaxValidatorStake = uint64(maxValidatorStakeMultiplier) * units.MegaAvax

	// Min delegator stake: 5 to 100 AVAX
	minDelegatorStakeMultiplier := 5 + rng.Intn(96)
	params.StakingConfig.MinDelegatorStake = uint64(minDelegatorStakeMultiplier) * units.Avax

	// Min delegation fee: 1% to 10%
	minDelegationFeePercent := 1 + rng.Intn(10)
	params.StakingConfig.MinDelegationFee = uint32(minDelegationFeePercent * 10000) // Convert to basis points

	return params
}

// GetRandomizedParams returns randomized network parameters if ANTITHESIS_RANDOM_SEED is set,
// otherwise returns the original parameters for the given network.
func GetRandomizedParams(networkID uint32) genesis.Params {
	baseParams := genesis.Params{
		TxFeeConfig:   genesis.GetTxFeeConfig(networkID),
		StakingConfig: genesis.GetStakingConfig(networkID),
	}

	// Check for antithesis random seed
	antithesisSeed := os.Getenv("ANTITHESIS_RANDOM_SEED")
	if antithesisSeed == "" {
		return baseParams
	}

	// Parse the seed and create a deterministic random source
	seed, err := strconv.ParseInt(antithesisSeed, 10, 64)
	if err != nil {
		return baseParams
	}

	rng := rand.New(rand.NewSource(seed)) // #nosec G404
	return RandomizedParams(rng, baseParams)
}
