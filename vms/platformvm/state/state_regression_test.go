// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

func TestCantStateBeRebuilt(t *testing.T) {
	require := require.New(t)

	// elements commont to the two VMs
	var (
		_, genesisBytes = simpleGenesisWithASingleUTXO(t, snowtest.AVAXAssetID)
		stateBaseDB     = memdb.New()

		ctx = snowtest.Context(t, snowtest.PChainID)

		cfg = &config.Config{
			Chains:                 chains.TestManager,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		}

		rewards    = reward.NewCalculator(cfg.RewardConfig)
		registerer = prometheus.NewRegistry()

		dummyMetrics metrics.Metrics
		err          error
	)

	dummyMetrics, err = metrics.New("", registerer)
	require.NoError(err)

	// Instantiate state the first time
	firstDB := prefixdb.New([]byte{}, stateBaseDB)
	firstState, err := New(
		firstDB,
		genesisBytes,
		cfg,
		ctx,
		dummyMetrics,
		rewards,
	)
	require.NoError(err)
	require.NotNil(firstState)

	// Instantiate VM a second time
	secondDB := prefixdb.New([]byte{}, stateBaseDB)
	secondState, err := New(
		secondDB,
		genesisBytes,
		cfg,
		ctx,
		dummyMetrics,
		rewards,
	)
	require.NoError(err)
	require.NotNil(secondState)
}

func simpleGenesisWithASingleUTXO(t *testing.T, avaxAssetID ids.ID) (*api.BuildGenesisArgs, []byte) {
	require := require.New(t)

	id := ids.GenerateTestShortID()
	addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
	require.NoError(err)
	genesisUTXOs := []api.UTXO{
		{
			Amount:  json.Uint64(2000 * units.Avax),
			Address: addr,
		},
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(constants.UnitTestID),
		AvaxAssetID:   avaxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    make([]api.GenesisPermissionlessValidator, 0),
		Chains:        nil,
		Time:          json.Uint64(12345678),
		InitialSupply: json.Uint64(360 * units.MegaAvax),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return &buildGenesisArgs, genesisBytes
}
