// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/stretchr/testify/require"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avagovalidators "github.com/ava-labs/avalanchego/snow/validators"
)

func TestUptimeTracker(t *testing.T) {
	testNodeID := ids.GenerateTestNodeID()
	testValidationID := ids.GenerateTestID()
	baseTime := time.Unix(0, 0)
	startTime := uint64(baseTime.Unix())

	// TODO(JonathanOppenheimer): see func NewTestValidatorState() -- this should be examined
	// when we address the issue of that function.
	makeValidatorState := func() *validatorstest.State {
		return &validatorstest.State{
			GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*avagovalidators.GetCurrentValidatorOutput, uint64, error) {
				return map[ids.ID]*avagovalidators.GetCurrentValidatorOutput{
					testValidationID: {
						ValidationID:  testValidationID,
						NodeID:        testNodeID,
						PublicKey:     nil,
						Weight:        1,
						StartTime:     startTime,
						IsActive:      true,
						IsL1Validator: true,
					},
				}, 0, nil
			},
		}
	}

	require := require.New(t)
	ctx, dbManager, genesisBytes := setupGenesis(t, upgradetest.Latest)
	ctx.ValidatorState = makeValidatorState()
	appSender := &enginetest.Sender{T: t}

	vm := &VM{}
	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte(""),
		[]*commonEng.Fx{},
		appSender,
	))
	defer vm.Shutdown(context.Background())

	// Mock the clock to control time progression
	clock := vm.clock
	clock.Set(baseTime)

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))

	// After bootstrapping but before NormalOp, uptimeTracker hasn't started syncing yet
	// so GetUptime should not be able to find the validation ID
	_, _, err := vm.uptimeTracker.GetUptime(testValidationID)
	require.ErrorIs(err, uptimetracker.ErrValidationIDNotFound)

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// After transitioning to NormalOp, Sync() is called automatically to populate uptime
	// from validator state, so GetUptime should work now
	// The validator starts with 0 uptime since it has not connected yet
	initialUptime, initialLastUpdated, err := vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.Equal(time.Duration(0), initialUptime, "Initial uptime should be 0")
	require.Equal(baseTime, initialLastUpdated, "Initial lastUpdated should be baseTime")

	// connect, time passes
	require.NoError(vm.uptimeTracker.Connect(testNodeID))
	clock.Set(baseTime.Add(1 * time.Hour))

	// get uptime after 1 hour of being connected - uptime should have increased by 1 hour
	upDuration, lastUpdated, err := vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.Equal(1*time.Hour, upDuration, "Uptime should be 1 hour")
	require.Equal(baseTime.Add(1*time.Hour), lastUpdated, "lastUpdated should reflect new clock time")

	// disconnect, time passes another 2 hours
	require.NoError(vm.uptimeTracker.Disconnect(testNodeID))
	clock.Set(baseTime.Add(2 * time.Hour))

	// get uptime - should not have increased since we were disconnected
	upDuration, lastUpdated, err = vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.Equal(1*time.Hour, upDuration, "Uptime should not increase while disconnected")
	require.Equal(baseTime.Add(2*time.Hour), lastUpdated, "lastUpdated should still advance")

	// reconnect, time passes another 30 minutes
	require.NoError(vm.uptimeTracker.Connect(testNodeID))
	clock.Set(baseTime.Add(2*time.Hour + 30*time.Minute))

	// get uptime - total uptime should be 1h30m
	upDuration, lastUpdated, err = vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.Equal(1*time.Hour+30*time.Minute, upDuration,
		"Uptime should be 1h30m (1h from first connection + 30m from second)")
	require.Equal(baseTime.Add(2*time.Hour+30*time.Minute), lastUpdated,
		"lastUpdated should reflect final clock time")
}
