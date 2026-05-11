// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

const numValidators = 5_000

type testExternalHandler struct{}

func (*testExternalHandler) Connected(ids.NodeID, *version.Application, ids.ID) {}

func (*testExternalHandler) Disconnected(ids.NodeID) {}

func (*testExternalHandler) HandleInbound(context.Context, *message.InboundMessage) {}

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

	b := beaconManager{
		ExternalHandler:         &testExternalHandler{},
		beacons:                 validatorSet,
		requiredConns:           numValidators,
		onSufficientlyConnected: make(chan struct{}),
	}

	// connect numValidators validators, each with a weight of 1
	wg.Add(numValidators)
	for _, nodeID := range validatorIDs {
		go func() {
			defer wg.Done()
			b.Connected(nodeID, version.Current, constants.PrimaryNetworkID)
			b.Connected(nodeID, version.Current, ids.GenerateTestID())
		}()
	}
	wg.Wait()

	// we should have a weight of numValidators now
	require.Equal(int64(numValidators), b.numConns)

	// disconnect numValidators validators
	wg.Add(numValidators)
	for _, nodeID := range validatorIDs {
		go func() {
			defer wg.Done()
			b.Disconnected(nodeID)
		}()
	}
	wg.Wait()

	// we should a weight of zero now
	require.Zero(b.numConns)
}
