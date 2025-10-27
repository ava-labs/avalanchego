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
	"github.com/stretchr/testify/require"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	avagovalidators "github.com/ava-labs/avalanchego/snow/validators"
)

func TestUptimeTracker(t *testing.T) {
	testNodeID := ids.GenerateTestNodeID()
	testValidationID := ids.GenerateTestID()
	startTime := uint64(time.Now().Unix())

	// TODO(JonathanOppenheimer): see func NewTestValidatorState() -- this should be examined
	// when we address the issue of that function.
	makeValidatorState := func() *validatorstest.State {
		return &validatorstest.State{
			GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*avagovalidators.GetCurrentValidatorOutput, uint64, error) {
				return map[ids.ID]*avagovalidators.GetCurrentValidatorOutput{
					testValidationID: {
						NodeID:    testNodeID,
						PublicKey: nil,
						Weight:    1,
						StartTime: startTime,
						IsActive:  true,
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

	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))

	// After bootstrapping but before NormalOp, uptimeTracker hasn't started syncing yet
	_, _, found, err := vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.False(found, "uptime should not be tracked yet")

	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// After transitioning to NormalOp, Sync() is called automatically to populate uptime
	// from validator state
	_, _, found, err = vm.uptimeTracker.GetUptime(testValidationID)
	require.NoError(err)
	require.True(found, "uptime should be tracked after transitioning to NormalOp")
}
