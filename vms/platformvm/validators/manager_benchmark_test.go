// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func BenchmarkGetValidatorSet(b *testing.B) {
	require := require.New(b)

	db, err := leveldb.New(
		b.TempDir(),
		nil,
		logging.NoLog{},
		"",
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	avaxAssetID := ids.GenerateTestID()
	genesisTime := time.Now().Truncate(time.Second)
	genesisEndTime := genesisTime.Add(28 * 24 * time.Hour)

	addr, err := address.FormatBech32(constants.UnitTestHRP, ids.GenerateTestShortID().Bytes())
	require.NoError(err)

	genesisValidators := []api.PermissionlessValidator{{
		Staker: api.Staker{
			StartTime: json.Uint64(genesisTime.Unix()),
			EndTime:   json.Uint64(genesisEndTime.Unix()),
			NodeID:    ids.GenerateTestNodeID(),
		},
		RewardOwner: &api.Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []api.UTXO{{
			Amount:  json.Uint64(2 * units.KiloAvax),
			Address: addr,
		}},
		DelegationFee: reward.PercentDenominator,
	}}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         nil,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(genesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	vdrs := validators.NewManager()
	vdrs.Add(constants.PrimaryNetworkID, validators.NewSet())

	execConfig, err := config.GetExecutionConfig(nil)
	require.NoError(err)

	metrics, err := metrics.New("", prometheus.NewRegistry())
	require.NoError(err)

	s, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		&config.Config{
			Validators: vdrs,
		},
		execConfig,
		&snow.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    ids.GenerateTestNodeID(),
			Log:       logging.NoLog{},
		},
		metrics,
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
		new(utils.Atomic[bool]),
	)
	require.NoError(err)

	m := NewManager(
		logging.NoLog{},
		config.Config{},
		s,
		metrics,
		new(mockable.Clock),
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          ids.GenerateTestNodeID(),
		PublicKey:       bls.PublicFromSecretKey(sk),
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          2 * units.MegaAvax,
		StartTime:       genesisTime,
		EndTime:         genesisEndTime,
		PotentialReward: 0,
		NextTime:        genesisEndTime,
		Priority:        txs.PrimaryNetworkValidatorCurrentPriority,
	})

	blk, err := blocks.NewBanffStandardBlock(genesisTime, ids.GenerateTestID(), 1, nil)
	require.NoError(err)

	s.AddStatelessBlock(blk)
	s.SetHeight(1)
	require.NoError(s.Commit())

	_ = m
}
