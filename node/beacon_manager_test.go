// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router/routermock"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

const numValidators = 5_000

// Tests that reconnects that mutate the beacon manager's current total stake
// weight is consistent. Test is not deterministic.
func TestBeaconManager_DataRace(t *testing.T) {
	require := require.New(t)

	validatorIDs := make([]ids.NodeID, 0, numValidators)
	validatorSet := validators.NewManager()
	for i := 0; i < numValidators; i++ {
		nodeID := ids.GenerateTestNodeID()

		require.NoError(validatorSet.AddStaker(constants.PrimaryNetworkID, nodeID, nil, ids.Empty, 1))
		validatorIDs = append(validatorIDs, nodeID)
	}

	wg := &sync.WaitGroup{}

	ctrl := gomock.NewController(t)
	mockRouter := routermock.NewRouter(ctrl)

	b := beaconManager{
		Router:                  mockRouter,
		beacons:                 validatorSet,
		requiredConns:           numValidators,
		onSufficientlyConnected: make(chan struct{}),
	}

	// connect numValidators validators, each with a weight of 1
	wg.Add(2 * numValidators)
	mockRouter.EXPECT().
		Connected(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(2 * numValidators).
		Do(func(ids.NodeID, *version.Application, ids.ID) {
			wg.Done()
		})

	for _, nodeID := range validatorIDs {
		go func() {
			b.Connected(nodeID, version.CurrentApp, constants.PrimaryNetworkID)
			b.Connected(nodeID, version.CurrentApp, ids.GenerateTestID())
		}()
	}
	wg.Wait()

	// we should have a weight of numValidators now
	require.Equal(int64(numValidators), b.numConns)

	// disconnect numValidators validators
	wg.Add(numValidators)
	mockRouter.EXPECT().
		Disconnected(gomock.Any()).
		Times(numValidators).
		Do(func(ids.NodeID) {
			wg.Done()
		})

	for _, nodeID := range validatorIDs {
		go b.Disconnected(nodeID)
	}
	wg.Wait()

	// we should a weight of zero now
	require.Zero(b.numConns)
}
