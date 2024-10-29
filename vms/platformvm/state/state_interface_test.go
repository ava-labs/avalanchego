// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/statetest"
)

func TestState_GetEtnaHeight_Activation(t *testing.T) {
	require := require.New(t)

	upgrades := upgradetest.GetConfig(upgradetest.Durango)
	upgrades.EtnaTime = genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	state := statetest.New(t, statetest.Config{
		Upgrades: upgrades,
	})

	// Etna isn't initially active
	_, err := state.GetEtnaHeight()
	require.ErrorIs(err, database.ErrNotFound)

	// Etna still isn't active after advancing the time
	state.SetHeight(1)
	state.SetTimestamp(genesistest.DefaultValidatorStartTime.Add(time.Second))
	require.NoError(state.Commit())

	_, err = state.GetEtnaHeight()
	require.ErrorIs(err, database.ErrNotFound)

	// Etna was just activated
	const expectedEtnaHeight uint64 = 2
	state.SetHeight(expectedEtnaHeight)
	state.SetTimestamp(genesistest.DefaultValidatorStartTime.Add(2 * time.Second))
	require.NoError(state.Commit())

	etnaHeight, err := state.GetEtnaHeight()
	require.NoError(err)
	require.Equal(expectedEtnaHeight, etnaHeight)

	// Etna was previously activated
	state.SetHeight(3)
	state.SetTimestamp(genesistest.DefaultValidatorStartTime.Add(3 * time.Second))
	require.NoError(state.Commit())

	etnaHeight, err = state.GetEtnaHeight()
	require.NoError(err)
	require.Equal(expectedEtnaHeight, etnaHeight)
}

func TestState_GetEtnaHeight_InitiallyActive(t *testing.T) {
	require := require.New(t)

	state := statetest.New(t, statetest.Config{})

	// Etna is initially active
	etnaHeight, err := state.GetEtnaHeight()
	require.NoError(err)
	require.Zero(etnaHeight)
}
