// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
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
	ValidatorWeight2 = 5 * units.MilliAvax
	InitialBalance2  = 1 * units.Avax
	InitialSupply2   = 360 * units.MegaAvax
)

var (
	// Keys that are funded in the genesis
	FundedKeys = secp256k1.TestKeys()

	// Node IDs of genesis validators
	NodeIDs []ids.NodeID
)

func init() {
	NodeIDs = make([]ids.NodeID, len(FundedKeys))
	for i := range FundedKeys {
		NodeIDs[i] = ids.GenerateTestNodeID()
	}
}

func Build(t *testing.T) *api.BuildGenesisArgs {
	require := require.New(t)

	var (
		utxos      = make([]api.UTXO, len(FundedKeys))
		validators = make([]api.GenesisPermissionlessValidator, len(FundedKeys))
	)
	for i, key := range FundedKeys {
		addr, err := address.FormatBech32(constants.UnitTestHRP, key.Address().Bytes())
		require.NoError(err)

		utxos[i] = api.UTXO{
			Amount:  json.Uint64(InitialBalance2),
			Address: addr,
		}

		nodeID := NodeIDs[i]
		validators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(TimeUnix),
				EndTime:   json.Uint64(ValidatorEndTimeUnix),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(ValidatorWeight2),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	return &api.BuildGenesisArgs{
		AvaxAssetID:   snowtest.AVAXAssetID,
		NetworkID:     json.Uint32(constants.UnitTestID),
		UTXOs:         utxos,
		Validators:    validators,
		Time:          json.Uint64(TimeUnix),
		InitialSupply: json.Uint64(InitialSupply2),
		Encoding:      formatting.Hex,
	}
}

func BuildBytes(t *testing.T) []byte {
	require := require.New(t)

	buildGenesisArgs := Build(t)
	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return genesisBytes
}
