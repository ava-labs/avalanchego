package evm

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	avagoValidators "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/plugin/evm/validators"
	"github.com/ava-labs/subnet-evm/plugin/evm/validators/interfaces"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestValidatorState(t *testing.T) {
	require := require.New(t)
	genesis := &core.Genesis{}
	require.NoError(genesis.UnmarshalJSON([]byte(genesisJSONLatest)))
	genesisJSON, err := genesis.MarshalJSON()
	require.NoError(err)

	vm := &VM{}
	ctx, dbManager, genesisBytes, issuer, _ := setupGenesis(t, string(genesisJSON))
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
		GetCurrentValidatorSetF: func(ctx context.Context, subnetID ids.ID) (map[ids.ID]*avagoValidators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
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
		issuer,
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(err, "error initializing GenesisVM")

	// Test case 1: state should not be populated until bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.Equal(0, vm.validatorState.GetValidationIDs().Len())
	_, _, err = vm.uptimeManager.CalculateUptime(testNodeIDs[0])
	require.ErrorIs(database.ErrNotFound, err)
	require.False(vm.uptimeManager.StartedTracking())

	// Test case 2: state should be populated after bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))
	require.Len(vm.validatorState.GetValidationIDs(), 3)
	_, _, err = vm.uptimeManager.CalculateUptime(testNodeIDs[0])
	require.NoError(err)
	require.True(vm.uptimeManager.StartedTracking())

	// Test case 3: restarting VM should not lose state
	vm.Shutdown(context.Background())
	// Shutdown should stop tracking
	require.False(vm.uptimeManager.StartedTracking())

	vm = &VM{}
	err = vm.Initialize(
		context.Background(),
		utils.TestSnowContext(), // this context does not have validators state, making VM to source it from the database
		dbManager,
		genesisBytes,
		[]byte(""),
		[]byte(""),
		issuer,
		[]*commonEng.Fx{},
		appSender,
	)
	require.NoError(err, "error initializing GenesisVM")
	require.Len(vm.validatorState.GetValidationIDs(), 3)
	_, _, err = vm.uptimeManager.CalculateUptime(testNodeIDs[0])
	require.NoError(err)
	require.False(vm.uptimeManager.StartedTracking())

	// Test case 4: new validators should be added to the state
	newValidationID := ids.GenerateTestID()
	newNodeID := ids.GenerateTestNodeID()
	testState := &validatorstest.State{
		GetCurrentValidatorSetF: func(ctx context.Context, subnetID ids.ID) (map[ids.ID]*avagoValidators.GetCurrentValidatorOutput, uint64, error) {
			return map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
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
	vm.ctx.ValidatorState = testState
	// set VM as bootstrapped
	require.NoError(vm.SetState(context.Background(), snow.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), snow.NormalOp))

	// new validator should be added to the state eventually after validatorsLoadFrequency
	require.EventuallyWithT(func(c *assert.CollectT) {
		assert.Len(c, vm.validatorState.GetNodeIDs(), 4)
		newValidator, err := vm.validatorState.GetValidator(newValidationID)
		assert.NoError(c, err)
		assert.Equal(c, newNodeID, newValidator.NodeID)
	}, loadValidatorsFrequency*2, 5*time.Second)
}

func TestLoadNewValidators(t *testing.T) {
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
	tests := []struct {
		name                      string
		initialValidators         map[ids.ID]*avagoValidators.GetCurrentValidatorOutput
		newValidators             map[ids.ID]*avagoValidators.GetCurrentValidatorOutput
		registerMockListenerCalls func(*interfaces.MockStateCallbackListener)
		expectedLoadErr           error
	}{
		{
			name:                      "before empty/after empty",
			initialValidators:         map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{},
			newValidators:             map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{},
			registerMockListenerCalls: func(*interfaces.MockStateCallbackListener) {},
		},
		{
			name:              "before empty/after one",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
			},
		},
		{
			name: "before one/after empty",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{},
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be removed
				mock.EXPECT().OnValidatorRemoved(testValidationIDs[0], testNodeIDs[0]).Times(1)
			},
		},
		{
			name: "no change",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
			},
		},
		{
			name: "status and weight change and new one",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
					Weight:    1,
				},
			},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  false,
					StartTime: 0,
					Weight:    2,
				},
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[1],
					IsActive:  true,
					StartTime: 0,
				},
			},
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be updated
				mock.EXPECT().OnValidatorStatusUpdated(testValidationIDs[0], testNodeIDs[0], false).Times(1)
				// new validator will be added
				mock.EXPECT().OnValidatorAdded(testValidationIDs[1], testNodeIDs[1], uint64(0), true).Times(1)
			},
		},
		{
			name: "renew validation ID",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be removed
				mock.EXPECT().OnValidatorRemoved(testValidationIDs[0], testNodeIDs[0]).Times(1)
				// new validator will be added
				mock.EXPECT().OnValidatorAdded(testValidationIDs[1], testNodeIDs[0], uint64(0), true).Times(1)
			},
		},
		{
			name: "renew node ID",
			initialValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*avagoValidators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[1],
					IsActive:  true,
					StartTime: 0,
				},
			},
			expectedLoadErr: validators.ErrImmutableField,
			registerMockListenerCalls: func(mock *interfaces.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it won't be called since we don't track the node ID changes
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(tt *testing.T) {
			require := require.New(tt)
			db := memdb.New()
			validatorState, err := validators.NewState(db)
			require.NoError(err)

			// set initial validators
			for vID, validator := range test.initialValidators {
				err := validatorState.AddValidator(interfaces.Validator{
					ValidationID:   vID,
					NodeID:         validator.NodeID,
					Weight:         validator.Weight,
					StartTimestamp: validator.StartTime,
					IsActive:       validator.IsActive,
					IsSoV:          validator.IsSoV,
				})
				require.NoError(err)
			}
			// enable mock listener
			ctrl := gomock.NewController(tt)
			mockListener := interfaces.NewMockStateCallbackListener(ctrl)
			test.registerMockListenerCalls(mockListener)

			validatorState.RegisterListener(mockListener)
			// load new validators
			err = loadValidators(validatorState, test.newValidators)
			if test.expectedLoadErr != nil {
				require.Error(err)
				return
			}
			require.NoError(err)
			// check if the state is as expected
			require.Equal(len(test.newValidators), validatorState.GetValidationIDs().Len())
			for vID, validator := range test.newValidators {
				v, err := validatorState.GetValidator(vID)
				require.NoError(err)
				require.Equal(validator.NodeID, v.NodeID)
				require.Equal(validator.Weight, v.Weight)
				require.Equal(validator.StartTime, v.StartTimestamp)
				require.Equal(validator.IsActive, v.IsActive)
				require.Equal(validator.IsSoV, v.IsSoV)
			}
		})
	}
}
