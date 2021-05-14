// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	managermocks "github.com/ava-labs/avalanchego/database/manager/mocks"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
			Version:  version.NewDefaultVersion(1, 0, 0),
		},
		true,
	)
	chainDBManager.On("Current").Return(
		&manager.VersionedDatabase{
			Database: currentDB,
			Version:  version.NewDefaultVersion(1, 4, 4),
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
	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil); err != nil {
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

	// Case 1: Validator is in database v1.0.0 but not v1.4.4
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

	// Case 2: Validator is in database v1.0.0 and v1.4.4,
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

	// Case 3: Validator is in database v1.0.0 and v1.4.4,
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

func (um *uptimeMigrater1_4_4) prevVersionSetUptime(prevDB database.Database, validatorID ids.ShortID, uptime *uptimeV100) error {
	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), prevDB)
	defer uptimeDB.Close()

	uptimeBytes, err := Codec.Marshal(codecVersion, uptime)
	if err != nil {
		return err
	}

	return uptimeDB.Put(validatorID.Bytes(), uptimeBytes)
}

// Only used in testing. TODO move to test package.
// Add a staker to subnet [subnetID]
// A staker may be a validator or a delegator
func (um *uptimeMigrater1_4_4) prevVersionAddStaker(db database.Database, subnetID ids.ID, tx *rewardTxV100) error {
	var (
		staker   TimedTx
		priority byte
	)
	switch unsignedTx := tx.Tx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		staker = unsignedTx
		priority = lowPriority
	case *UnsignedAddSubnetValidatorTx:
		staker = unsignedTx
		priority = mediumPriority
	case *UnsignedAddValidatorTx:
		staker = unsignedTx
		priority = topPriority
	default:
		return fmt.Errorf("staker is unexpected type %T", tx.Tx.UnsignedTx)
	}

	txBytes, err := Codec.Marshal(codecVersion, tx)
	if err != nil {
		return err
	}

	txID := tx.Tx.ID() // Tx ID of this tx

	// Sorted by subnet ID then stop time then tx ID
	prefixStop := []byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix))
	prefixStopDB := prefixdb.NewNested(prefixStop, db)

	stopKey, err := um.prevVersionTimedTxKey(staker.EndTime(), priority, txID)
	if err != nil {
		// Close the DB, but ignore the error, as the parent error needs to be
		// returned.
		_ = prefixStopDB.Close()
		return fmt.Errorf("couldn't serialize validator key: %w", err)
	}

	errs := wrappers.Errs{}
	errs.Add(
		prefixStopDB.Put(stopKey, txBytes),
		prefixStopDB.Close(),
	)

	return errs.Err
}

// timedTxKey constructs the key to use for [txID] in stop and start prefix DBs
func (um *uptimeMigrater1_4_4) prevVersionTimedTxKey(time time.Time, priority byte, txID ids.ID) ([]byte, error) {
	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(time.Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(txID[:])
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	return p.Bytes, nil
}
