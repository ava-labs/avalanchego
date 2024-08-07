// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func State(
	t *testing.T,
	cfg *config.Config,
	ctx *snow.Context,
	db database.Database,
	rewards reward.Calculator,
	genesisBytes []byte,
) state.State {
	t.Helper()
	state, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		cfg,
		ctx,
		metrics.Noop,
		rewards,
		&utils.Atomic[bool]{},
	)
	require.NoError(t, err)
	// persist and reload to init a bunch of in-memory stuff
	state.SetHeight(0)
	require.NoError(t, state.Commit())
	return state
}

func StateConfigFromAPIConfig(apiCfg api.Camino) *state.CaminoConfig {
	return &state.CaminoConfig{
		VerifyNodeSignature: apiCfg.VerifyNodeSignature,
		LockModeBondDeposit: apiCfg.LockModeBondDeposit,
	}
}
