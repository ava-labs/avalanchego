// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"
	"time"

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
	defaultWeight             = 5 * units.MilliAvax
	defaultMinStakingDuration = 24 * time.Hour
	defaultMinValidatorStake  = 5 * units.MilliAvax
	defaultBalance            = 100 * defaultMinValidatorStake
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

func BuildGenesisTest(t *testing.T) []byte {
	require := require.New(t)

	genesisUTXOs := make([]api.UTXO, len(FundedKeys))
	for i, key := range FundedKeys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		require.NoError(err)
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(NodeIDs))
	for i, nodeID := range NodeIDs {
		addr := FundedKeys[i].Address()
		addrStr, err := address.FormatBech32(constants.UnitTestHRP, addr.Bytes())
		require.NoError(err)
		genesisValidators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(TimeUnix),
				EndTime:   json.Uint64(ValidatorEndTimeUnix),
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
		AvaxAssetID:   snowtest.AVAXAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(TimeUnix),
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
