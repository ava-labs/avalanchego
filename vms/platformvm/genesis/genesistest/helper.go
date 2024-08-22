// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

const (
	defaultWeight             = 5 * units.MilliAvax
	defaultMinStakingDuration = 24 * time.Hour
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
	defaultValidateDuration   = 20 * defaultMinStakingDuration
)

var (
	defaultGenesisTime       = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime = defaultGenesisTime
	defaultValidateEndTime   = defaultValidateStartTime.Add(defaultValidateDuration)
	preFundedKeys            = secp256k1.TestKeys()

	// Node IDs of genesis validators. Initialized in init function
	GenesisNodeIDs []ids.NodeID
)

func init() {
	GenesisNodeIDs = make([]ids.NodeID, len(preFundedKeys))
	for i := range preFundedKeys {
		GenesisNodeIDs[i] = ids.GenerateTestNodeID()
	}
}

func BuildGenesisTest(t *testing.T, avaxAssetID ids.ID) []byte {
	require := require.New(t)

	genesisUTXOs := make([]api.UTXO, len(preFundedKeys))
	for i, key := range preFundedKeys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		require.NoError(err)
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(GenesisNodeIDs))
	for i, nodeID := range GenesisNodeIDs {
		addr := preFundedKeys[i].Address()
		addrStr, err := address.FormatBech32(constants.UnitTestHRP, addr.Bytes())
		require.NoError(err)
		genesisValidators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addrStr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addrStr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return genesisBytes
}
