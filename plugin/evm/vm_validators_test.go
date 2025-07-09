// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	avagovalidators "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/plugin/evm/validators"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatorState(t *testing.T) {
	require := require.New(t)
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONLatest)))
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)

	vm := &VM{}
	ctx, dbManager, genesisBytes, _ := setupGenesis(t, string(genesisJSON))
	appSender := &enginetest.Sender{T: t}
	appSender.CantSendAppGossip = true
	testNodeIDs := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	testValidationIDs := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}
	ctx.ValidatorState = &validatorstest.State{
		GetCurrentValidatorSetF: func(ctx context.Context, subnetID ids.ID) (map[ids.ID]*avagovalidators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*avagovalidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					PublicKey: nil,
					Weight:    1,
				},
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[1],
					PublicKey: nil,
					Weight:    1,
				},
				testValidationIDs[2]: {
					NodeID:    testNodeIDs[2],
					PublicKey: nil,
					Weight:    1,
				},
			}, 0, nil
		},
	}
	appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error { return nil }
	err = vm.Initialize(
		context.Background(),
		ctx,
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte(""),
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(err, "error initializing GenesisVM")

	// Test case 1: state should not be populated until bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.Equal(0, vm.validatorsManager.GetValidationIDs().Len())
	_, _, err = vm.validatorsManager.CalculateUptime(testNodeIDs[0])
	require.ErrorIs(database.ErrNotFound, err)
	require.False(vm.validatorsManager.StartedTracking())

	// Test case 2: state should be populated after bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))
	require.Len(vm.validatorsManager.GetValidationIDs(), 3)
	_, _, err = vm.validatorsManager.CalculateUptime(testNodeIDs[0])
	require.NoError(err)
	require.True(vm.validatorsManager.StartedTracking())

	// Test case 3: restarting VM should not lose state
	vm.Shutdown(context.Background())
	// Shutdown should stop tracking
	require.False(vm.validatorsManager.StartedTracking())

	vm = &VM{}
	err = vm.Initialize(
		context.Background(),
		utils.TestSnowContext(), // this context does not have validators state, making VM to source it from the database
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte(""),
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(err, "error initializing GenesisVM")
	require.Len(vm.validatorsManager.GetValidationIDs(), 3)
	_, _, err = vm.validatorsManager.CalculateUptime(testNodeIDs[0])
	require.NoError(err)
	require.False(vm.validatorsManager.StartedTracking())

	// Test case 4: new validators should be added to the state
	newValidationID := ids.GenerateTestID()
	newNodeID := ids.GenerateTestNodeID()
	testState := &validatorstest.State{
		GetCurrentValidatorSetF: func(ctx context.Context, subnetID ids.ID) (map[ids.ID]*avagovalidators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*avagovalidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					PublicKey: nil,
					Weight:    1,
				},
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[1],
					PublicKey: nil,
					Weight:    1,
				},
				testValidationIDs[2]: {
					NodeID:    testNodeIDs[2],
					PublicKey: nil,
					Weight:    1,
				},
				newValidationID: {
					NodeID:    newNodeID,
					PublicKey: nil,
					Weight:    1,
				},
			}, 0, nil
		},
	}
	// set VM as bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	vm.ctx.ValidatorState = testState

	// new validator should be added to the state eventually after SyncFrequency
	require.EventuallyWithT(func(c *assert.CollectT) {
		vm.vmLock.Lock()
		defer vm.vmLock.Unlock()
		assert.Len(c, vm.validatorsManager.GetNodeIDs(), 4)
		newValidator, err := vm.validatorsManager.GetValidator(newValidationID)
		assert.NoError(c, err)
		assert.Equal(c, newNodeID, newValidator.NodeID)
	}, validators.SyncFrequency*2, 5*time.Second)
}
