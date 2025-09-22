// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"errors"
	"math/big"
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
