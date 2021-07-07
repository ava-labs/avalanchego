// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	managermocks "github.com/ava-labs/avalanchego/database/manager/mocks"
)

// // Test that the migrater migrates a validator's uptime
func TestMigrateUptime(t *testing.T) {
	previousDB := memdb.New()
	defer previousDB.Close()
	currentDB := memdb.New()
	defer previousDB.Close()
	chainDBManager := &managermocks.Manager{}
	chainDBManager.On("Previous").Return(
		&manager.VersionedDatabase{
			Database: previousDB,
			Version:  version.DatabaseVersion1_0_0,
		},
		true,
	)
	chainDBManager.On("Current").Return(
		&manager.VersionedDatabase{
			Database: currentDB,
			Version:  version.DatabaseVersion1_4_5,
		},
	)
	chainDBManager.On("Close").Return(nil)

	// Setup the VM
	vm := &VM{}
	now := time.Now()
	vm.clock.Set(now)
	vm.Validators = validators.NewManager()
	ctx := defaultContext()
	ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()
	msgChan := make(chan common.Message, 1)
	_, genesisBytes := defaultGenesis()
	vm.StakeMintingPeriod = 365 * 24 * time.Hour
	shutdownNodeFunc := func(int) {
		t.Fatal("should not have called shutdown")
	}
	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil, shutdownNodeFunc); err != nil {
		t.Fatal(err)
	}

	// Insert the pre-upgrade validators into the pre-upgrade database
	stopPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, previousDB)
	defer stopDB.Close()
	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), previousDB)
	defer uptimeDB.Close()
	nodeID := ids.GenerateTestShortID()
	tx := Tx{
		&UnsignedAddValidatorTx{
			BaseTx: BaseTx{},
			Validator: Validator{
				NodeID: nodeID,
			},
			Stake:        nil,
			RewardsOwner: &secp256k1fx.OutputOwners{},
		},
		nil, // credentials
	}
	err := tx.Sign(vm.codec, nil)
	assert.NoError(t, err)
	rewardTx := rewardTxV100{
		Reward: 1337,
		Tx:     tx,
	}
	txBytes, err := vm.codec.Marshal(codecVersion, rewardTx)
	assert.NoError(t, err)
	err = stopDB.Put(utils.RandomBytes(32), txBytes)
	assert.NoError(t, err)

	oldUptime := &uptimeV100{
		UpDuration:  1337,
		LastUpdated: uint64(now.Add(-1 * time.Minute).Unix()),
	}
	uptimeBytes, err := vm.codec.Marshal(codecVersion, oldUptime)
	assert.NoError(t, err)
	err = uptimeDB.Put(nodeID.Bytes(), uptimeBytes)
	assert.NoError(t, err)

	// Case 1: Validator is in database v1.0.0 but not v1.4.5
	// Set up mocked VM state
	mockCurrentStakerChainState := &mockCurrentStakerChainState{}
	mockCurrentStakerChainState.Test(t)
	mockCurrentStakerChainState.On("GetStaker", mock.Anything).Return(nil, uint64(0), database.ErrNotFound).Once()
	mockInternalState := &MockInternalState{}
	mockInternalState.Test(t)
	mockInternalState.On("IsMigrated").Return(false, nil)
	mockInternalState.On("SetMigrated").Return(nil)
	mockInternalState.On("CurrentStakerChainState").Return(mockCurrentStakerChainState)
	mockInternalState.On("Commit").Return(nil)
	mockInternalState.On("Close").Return(nil)
	vm.internalState = mockInternalState

	// Do the migration
	assert.NoError(t, vm.migrateUptimes())
	// Uptime shouldn't have migrated because validator is not in current validator set
	mockInternalState.AssertNumberOfCalls(t, "IsMigrated", 1)
	mockInternalState.AssertNumberOfCalls(t, "SetMigrated", 1)
	mockInternalState.AssertNotCalled(t, "SetUptime")
	mockCurrentStakerChainState.AssertNumberOfCalls(t, "GetStaker", 1)

	// Case 2: Validator is in database v1.0.0 and v1.4.5,
	// and the lastUpdated value in the database v1.0.0 <= now
	// Should update lastUpdated and upDuration
	// Add the node to the current validator set
	mockCurrentStakerChainState.On("GetStaker", tx.ID()).Return(nil, uint64(0), nil)
	mockInternalState.On("CurrentStakerChainState").Return(mockCurrentStakerChainState)

	expectedDurationOffline := now.Sub(time.Unix(int64(oldUptime.LastUpdated), 0))
	expectedUpDuration := oldUptime.UpDuration*uint64(time.Second) + uint64(expectedDurationOffline.Nanoseconds())
	mockInternalState.On("SetUptime", nodeID, mock.AnythingOfType("time.Duration"), mock.AnythingOfType("time.Time")).Run(
		func(args mock.Arguments) {
			// Make sure the expected args are being passed in
			assert.EqualValues(t, expectedUpDuration, args[1])
			assert.EqualValues(t, now, args[2])
		},
	).Return(nil).Once()

	// Do the migration
	assert.NoError(t, vm.migrateUptimes())
	mockInternalState.AssertNumberOfCalls(t, "IsMigrated", 2)
	mockInternalState.AssertNumberOfCalls(t, "SetMigrated", 2)
	mockCurrentStakerChainState.AssertNumberOfCalls(t, "GetStaker", 2)
	mockInternalState.AssertNumberOfCalls(t, "SetUptime", 1)

	// Case 3: Validator is in database v1.0.0 and v1.4.5,
	// and the lastUpdated value in the database v1.0.0 > now
	vm.clock.Set(time.Unix(int64(oldUptime.LastUpdated-1), 0)) // Set VM's clock to before lastUpdated in old database
	mockInternalState.On("SetUptime", nodeID, mock.AnythingOfType("time.Duration"), mock.AnythingOfType("time.Time")).Run(
		func(args mock.Arguments) {
			// Make sure the expected args are being passed in
			// Should be migrating the old uptime from seconds to nanoseconds but otherwise unchanged
			// Should keep lastUpdated the same as in database v1.0.0
			assert.EqualValues(t, oldUptime.UpDuration*uint64(time.Second), args[1])
			assert.EqualValues(t, time.Unix(int64(oldUptime.LastUpdated), 0), args[2])
		},
	).Return(nil).Once()
	// Do the migration
	assert.NoError(t, vm.migrateUptimes())
}
