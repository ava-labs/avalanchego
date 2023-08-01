// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"math/rand"
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

// BenchmarkGetValidatorSet generates 10k diffs and calculates the time to
// generate the genesis validator set by applying them.
//
// This generates a single diff for each height. In practice there could be
// multiple or zero diffs at a given height.
//
// Note: BenchmarkGetValidatorSet gets the validator set of a subnet rather than
// the primary network because the primary network performs caching that would
// interfere with the benchmark.
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
	defer func() {
		require.NoError(db.Close())
	}()

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
		config.Config{
			Validators: vdrs,
		},
		s,
		metrics,
		new(mockable.Clock),
	)

	var (
		nodeIDs       []ids.NodeID
		currentHeight uint64
	)
	for i := 0; i < 50; i++ {
		currentHeight++
		nodeID, err := addPrimaryValidator(s, genesisTime, genesisEndTime, currentHeight)
		require.NoError(err)
		nodeIDs = append(nodeIDs, nodeID)
	}
	subnetID := ids.GenerateTestID()
	for _, nodeID := range nodeIDs {
		currentHeight++
		require.NoError(addSubnetValidator(s, subnetID, genesisTime, genesisEndTime, nodeID, currentHeight))
	}
	for i := 0; i < 9900; i++ {
		currentHeight++
		require.NoError(addSubnetDelegator(s, subnetID, genesisTime, genesisEndTime, nodeIDs, currentHeight))
	}

	ctx := context.Background()
	height, err := m.GetCurrentHeight(ctx)
	require.NoError(err)
	require.Equal(currentHeight, height)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := m.GetValidatorSet(ctx, 0, subnetID)
		require.NoError(err)
	}

	b.StopTimer()
}

func addPrimaryValidator(
	s state.State,
	startTime time.Time,
	endTime time.Time,
	height uint64,
) (ids.NodeID, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return ids.EmptyNodeID, err
	}

	nodeID := ids.GenerateTestNodeID()
	s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		PublicKey:       bls.PublicFromSecretKey(sk),
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          2 * units.MegaAvax,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.PrimaryNetworkValidatorCurrentPriority,
	})

	blk, err := blocks.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return ids.EmptyNodeID, err
	}

	s.AddStatelessBlock(blk)
	s.SetHeight(height)
	return nodeID, s.Commit()
}

func addSubnetValidator(
	s state.State,
	subnetID ids.ID,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	height uint64,
) error {
	s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1 * units.Avax,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.SubnetPermissionlessValidatorCurrentPriority,
	})

	blk, err := blocks.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return err
	}

	s.AddStatelessBlock(blk)
	s.SetHeight(height)
	return s.Commit()
}

func addSubnetDelegator(
	s state.State,
	subnetID ids.ID,
	startTime time.Time,
	endTime time.Time,
	nodeIDs []ids.NodeID,
	height uint64,
) error {
	i := rand.Intn(len(nodeIDs)) //#nosec G404
	nodeID := nodeIDs[i]
	s.PutCurrentDelegator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1 * units.Avax,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.SubnetPermissionlessDelegatorCurrentPriority,
	})

	blk, err := blocks.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return err
	}

	s.AddStatelessBlock(blk)
	s.SetLastAccepted(blk.ID())
	s.SetHeight(height)
	return s.Commit()
}
