// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/manager/mocks"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/core"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

// Test that validator uptimes are correctly migrated from
// database version 1.0.0 to database version 1.1.0
func TestMigrateUptime110(t *testing.T) {
	now := time.Now()

	// A validator whose uptime we migrate from an old database version
	validator1NodeID := ids.GenerateTestShortID()
	validator1Tx := &rewardTx{
		Reward: 1,
		Tx: Tx{
			UnsignedTx: &UnsignedAddValidatorTx{
				Validator: Validator{
					NodeID: validator1NodeID,
					Start:  uint64(now.Add(-1 * time.Minute).Unix()),
					End:    uint64(now.Add(3 * defaultMinStakingDuration).Unix()),
				},
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
		},
	}
	validator1TxBytes, err := Codec.Marshal(codecVersion, validator1Tx)
	assert.NoError(t, err)
	assert.NotNil(t, validator1TxBytes)
	validator1Uptime := validatorUptime{
		UpDuration:  12345,
		LastUpdated: uint64(now.Add(-1 * time.Minute).Unix()),
	}

	// A validator whose uptime we migrate from an old database version
	validator2NodeID := ids.GenerateTestShortID()
	validator2Tx := &rewardTx{
		Reward: 2,
		Tx: Tx{
			UnsignedTx: &UnsignedAddValidatorTx{
				Validator: Validator{
					NodeID: validator2NodeID,
					Start:  uint64(now.Add(-5 * time.Minute).Unix()),
					End:    uint64(now.Add(2 * defaultMinStakingDuration).Unix()),
				},
				RewardsOwner: &secp256k1fx.OutputOwners{},
			},
		},
	}
	validator2TxBytes, err := Codec.Marshal(codecVersion, validator2Tx)
	assert.NoError(t, err)
	assert.NotNil(t, validator2TxBytes)
	validator2Uptime := validatorUptime{
		UpDuration:  54321,
		LastUpdated: uint64(now.Add(-2 * time.Minute).Unix()),
	}

	previousDB := memdb.New()
	currentDB := memdb.New()
	chainDBManager := &mocks.Manager{}
	chainDBManager.On("Previous").Return(
		&manager.VersionedDatabase{
			Database: previousDB,
			Version:  version.DefaultVersion1,
		},
		true,
	)
	chainDBManager.On("Current").Return(
		&manager.VersionedDatabase{
			Database: currentDB,
			Version:  version.DefaultVersion2,
		},
	)
	vm := &VM{
		SnowmanVM:          &core.SnowmanVM{},
		minValidatorStake:  defaultMinValidatorStake,
		maxValidatorStake:  defaultMaxValidatorStake,
		minDelegatorStake:  defaultMinDelegatorStake,
		minStakeDuration:   defaultMinStakingDuration,
		maxStakeDuration:   defaultMaxStakingDuration,
		stakeMintingPeriod: defaultMaxStakingDuration,
	}
	vm.clock.Set(now)
	vm.vdrMgr = validators.NewManager()
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
	if err := vm.Initialize(ctx, chainDBManager, genesisBytes, nil, nil, msgChan, nil); err != nil {
		t.Fatal(err)
	}

	// Mark that in an old database version, validator 1 and validator 2 have uptimes
	// associated with them
	assert.NoError(t, vm.setUptime(previousDB, validator1NodeID, &validator1Uptime))
	assert.NoError(t, vm.setUptime(previousDB, validator2NodeID, &validator2Uptime))

	// Mark these validators as current validators
	assert.NoError(t, vm.addStaker(vm.DB, constants.PrimaryNetworkID, validator1Tx))
	assert.NoError(t, vm.addStaker(vm.DB, constants.PrimaryNetworkID, validator2Tx))

	assert.NoError(t, vm.Bootstrapping())
	assert.NoError(t, vm.Bootstrapped()) // Calling bootstrapped triggers off the migration

	// Check that uptimes were migrated correctly
	{
		uptime, err := vm.uptime(vm.DB, validator1NodeID)
		assert.NoError(t, err)
		// expected up duration is old up duration plus the amount of time we were "offline" (the VM's bootstrap time
		// minus the old "last updated" time
		durationOffline := vm.bootstrappedTime.Sub(time.Unix(int64(validator1Uptime.LastUpdated), 0))
		expectedUpDuration := validator1Uptime.UpDuration + uint64(durationOffline.Seconds())
		assert.EqualValues(t, uptime.UpDuration, expectedUpDuration)
		assert.EqualValues(t, uptime.LastUpdated, uint64(now.Unix()))
	}
	{
		uptime, err := vm.uptime(vm.DB, validator2NodeID)
		assert.NoError(t, err)
		// expected up duration is old up duration plus the amount of time we were "offline" (the VM's bootstrap time
		// minus the old "last updated" time
		expectedUpDuration := validator2Uptime.UpDuration + uint64(vm.bootstrappedTime.Sub(time.Unix(int64(validator2Uptime.LastUpdated), 0)).Seconds())
		assert.EqualValues(t, uptime.UpDuration, expectedUpDuration)
		assert.EqualValues(t, uptime.LastUpdated, uint64(now.Unix()))
	}

}
