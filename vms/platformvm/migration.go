// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

const (
	stopDBPrefix   = "stop"
	uptimeDBPrefix = "uptime"
)

func (vm *VM) migrateUptimes() error {
	prevDB, prevDBExists := vm.dbManager.Previous()
	if !prevDBExists { // there is nothing to migrate
		vm.ctx.Log.Info("not doing uptime migration")
		return nil
	}
	prevDBVersion := prevDB.Version
	currentDB := vm.dbManager.Current()
	currentDBVersion := currentDB.Version

	// Only valid migration is from database version 1.0.0 to 1.4.3
	if prevDBVersion.Compare(version.NewDefaultVersion(1, 0, 0)) == 0 &&
		currentDBVersion.Compare(version.NewDefaultVersion(1, 4, 3)) == 0 {
		migrater := uptimeMigrater143{vm: vm}
		return migrater.migrate(prevDB)
	}
	return nil
}

type uptimeMigrater143 struct {
	vm *VM
}

// migrate validator uptimes from database version 1.0.0 to database version 1.4.3.
func (um *uptimeMigrater143) migrate(dbV100 *manager.VersionedDatabase) error {
	migrated, err := um.vm.internalState.IsMigrated()
	if err != nil {
		return fmt.Errorf("couldn't get whether the v1.0.0 --> v1.4.3 database migration has occurred: %s", err)
	} else if migrated { // already did migration
		um.vm.ctx.Log.Info("not doing uptime migration")
		return nil
	}
	now := um.vm.clock.Time()

	um.vm.ctx.Log.Info("migrating validator uptimes from database v1.0.0 to v1.4.3")
	stopPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, dbV100)
	defer stopDB.Close()

	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), dbV100)
	defer uptimeDB.Close()

	stopDBIter := stopDB.NewIterator()
	defer stopDBIter.Release()

	cs := um.vm.internalState.CurrentStakerChainState()
	// Iterate over all the uptimes in the v1.0.0 database
	for stopDBIter.Next() {
		txBytes := stopDBIter.Value()
		tx := rewardTxV100{}

		if _, err := um.vm.codec.Unmarshal(txBytes, &tx); err != nil {
			um.vm.ctx.Log.Warn("couldn't unmarshal validator tx from database v1.0.0: %s", err)
			continue
		}
		if err := tx.Tx.Sign(um.vm.codec, nil); err != nil {
			um.vm.ctx.Log.Warn("couldn't initialize validator tx from database v1.0.0: %s", err)
			continue
		}
		addVdrTx, ok := tx.Tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			continue
		}

		nodeID := addVdrTx.Validator.ID()
		uptimeV100, err := um.previousVersionGetUptime(dbV100, nodeID)
		if err != nil {
			um.vm.ctx.Log.Warn("couldn't get uptime for node %s from database v1.0.0: %s", nodeID.PrefixedString(constants.NodeIDPrefix), err)
			continue
		}

		// only migrate a validator's uptime if the validator is still in the validator set.
		if _, _, err := cs.GetStaker(addVdrTx.ID()); err != nil {
			// This validator isn't in the current validator set. They must have left the validator set. Ignore.
			continue
		}

		// In v1.0.0, up duration is stored in seconds. In v1.4.3, it is stored in nanoseconds.
		// Convert from seconds to nanoseconds by multiplying by uint64(time.Second)
		upDuration := uptimeV100.UpDuration * uint64(time.Second)
		// Update the validator's uptime
		lastUpdated := time.Unix(int64(uptimeV100.LastUpdated), 0)
		if now.After(lastUpdated) {
			durationOffline := now.Sub(lastUpdated)
			upDuration += uint64(durationOffline.Nanoseconds())
			lastUpdated = now
		}

		um.vm.ctx.Log.Debug(
			"migrating uptime for node %s (tx %s) from database v1.0.0 to v1.4.3. Uptime: %s. Last updated: %s",
			nodeID.PrefixedString(constants.NodeIDPrefix),
			addVdrTx.ID(),
			time.Duration(upDuration),
			lastUpdated,
		)
		if err := um.vm.internalState.SetUptime(nodeID, time.Duration(upDuration), lastUpdated); err != nil {
			um.vm.ctx.Log.Warn("couldn't migrate uptime for node %s: %s", nodeID.PrefixedString(constants.NodeIDPrefix), err)
			continue
		}
	}
	if err = stopDBIter.Error(); err != nil {
		return err
	}
	if err := um.vm.internalState.SetMigrated(); err != nil {
		return fmt.Errorf("couldn't set migrated flag: %s", err)
	}
	if err := um.vm.internalState.Commit(); err != nil {
		return fmt.Errorf("couldn't commit state: %s", err)
	}
	um.vm.ctx.Log.Info("finished migrating platformvm from database v1.0.0 to v1.4.3")
	return nil
}

func (um *uptimeMigrater143) previousVersionGetUptime(db database.Database, nodeID ids.ShortID) (*uptimeV100, error) {
	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), db)
	defer uptimeDB.Close()

	uptimeBytes, err := uptimeDB.Get(nodeID.Bytes())
	if err != nil {
		return nil, err
	}

	uptime := uptimeV100{}
	if _, err = Codec.Unmarshal(uptimeBytes, &uptime); err != nil {
		return nil, err
	}
	return &uptime, uptimeDB.Close()
}

// Only used in testing. TODO move this to the test package.
func (um *uptimeMigrater143) prevVersionSetUptime(prevDB database.Database, validatorID ids.ShortID, uptime *uptimeV100) error {
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
func (um *uptimeMigrater143) prevVersionAddStaker(db database.Database, subnetID ids.ID, tx *rewardTxV100) error {
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
func (um *uptimeMigrater143) prevVersionTimedTxKey(time time.Time, priority byte, txID ids.ID) ([]byte, error) {
	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(time.Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(txID[:])
	if p.Err != nil {
		return nil, fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	return p.Bytes, nil
}

// only migrate if the id of the old tx is the same as the current tx
func (um *uptimeMigrater143) shouldMigrate(previousNodeID ids.ShortID, previousTxID ids.ID, currentStakersTxs []*Tx) (bool, error) {
	_, _, err := um.vm.internalState.GetUptime(previousNodeID)
	switch {
	case err != nil && err == database.ErrNotFound:
		return false, nil
	case err != nil:
		return false, err
	}

	// find staking tx that exists on both dbs
	for _, tx := range currentStakersTxs {
		currentStakerTx, ok := tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			continue
		}
		if currentStakerTx.Validator.NodeID == previousNodeID {
			return true, nil
		}
	}

	return false, nil
}

type uptimeV100 struct {
	UpDuration  uint64 `serialize:"true"` // In seconds
	LastUpdated uint64 `serialize:"true"` // Unix time in seconds
}

type rewardTxV100 struct {
	Reward uint64 `serialize:"true"`
	Tx     Tx     `serialize:"true"`
}
