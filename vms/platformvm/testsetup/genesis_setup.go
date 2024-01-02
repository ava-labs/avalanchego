// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

// Shared Unit test setup utilities for a platform vm packages

var (
	// each key controls an address that has [Balance] AVAX at genesis
	Keys = secp256k1.TestKeys()

	// many UTs add a subnet right after genesis. They should use
	// SubnetControlKeys to control the subnet
	SubnetControlKeys = Keys[0:3]

	// Node IDs of genesis validators. Initialized in init function
	GenesisNodeIDs []ids.NodeID

	// chain timestamp at genesis
	GenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	ValidateStartTime  = GenesisTime
	MinStakingDuration = 24 * time.Hour
	MaxStakingDuration = 365 * 24 * time.Hour

	// time that genesis validators stop validating
	ValidateEndTime = ValidateStartTime.Add(20 * MinStakingDuration)

	MinValidatorStake = 5 * units.MilliAvax
	MaxValidatorStake = 500 * units.MilliAvax

	GenesisUTXOBalance     = 100 * MinValidatorStake // amount in each genesis utxos
	GenesisValidatorWeight = MinValidatorStake       // weight of each genesis validator
)

func init() {
	for _, key := range Keys {
		// TODO: use ids.GenerateTestNodeID() instead of ids.BuildTestNodeID
		// Can be done when TestGetState is refactored
		nodeBytes := key.PublicKey().Address()
		nodeID := ids.BuildTestNodeID(nodeBytes[:])

		GenesisNodeIDs = append(GenesisNodeIDs, nodeID)
	}
}

// [BuildGenesis] is a good default to build genesis for platformVM unit tests
// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func BuildGenesis(t testing.TB, ctx *snow.Context) (*api.BuildGenesisArgs, []byte) {
	require := require.New(t)

	genesisUTXOs := make([]api.UTXO, len(Keys))
	for i, key := range Keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		require.NoError(err)
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(GenesisUTXOBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(GenesisNodeIDs))
	for i, nodeID := range GenesisNodeIDs {
		addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
		require.NoError(err)
		genesisValidators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(ValidateStartTime.Unix()),
				EndTime:   json.Uint64(ValidateEndTime.Unix()),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(GenesisValidatorWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   ctx.AVAXAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(GenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return &buildGenesisArgs, genesisBytes
}
