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

	// Only valid migration is from database version 1.0.0 to 1.4.4
	if prevDBVersion.Compare(version.DatabaseVersion1_0_0) == 0 &&
		currentDBVersion.Compare(version.DatabaseVersion1_4_4) == 0 {
		migrater := uptimeMigrater1_4_4{vm: vm}
		return migrater.migrate(prevDB)
	}
	return nil
}

type uptimeMigrater1_4_4 struct {
	vm *VM
}

// migrate validator uptimes from database version 1.0.0 to database version 1.4.4.
func (um *uptimeMigrater1_4_4) migrate(dbV100 *manager.VersionedDatabase) error {
	migrated, err := um.vm.internalState.IsMigrated()
	if err != nil {
		return fmt.Errorf("couldn't get whether the v1.0.0 --> v1.4.4 database migration has occurred: %s", err)
	} else if migrated { // already did migration
		um.vm.ctx.Log.Info("not doing uptime migration")
		return nil
	}
	now := um.vm.clock.Time()

	um.vm.ctx.Log.Info("migrating validator uptimes from database v1.0.0 to v1.4.4")
	stopPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, dbV100.Database)
	defer stopDB.Close()

	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), dbV100.Database)
	defer uptimeDB.Close()

	stopDBIter := stopDB.NewIterator()
	defer stopDBIter.Release()

	cs := um.vm.internalState.CurrentStakerChainState()
	// Iterate over all the uptimes in the v1.0.0 database
	for stopDBIter.Next() {
		txBytes := stopDBIter.Value()
		tx := rewardTxV100{}

		if _, err := um.vm.codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx from database v1.0.0: %s", err)
		}
		if err := tx.Tx.Sign(um.vm.codec, nil); err != nil {
			return fmt.Errorf("couldn't initialize validator tx from database v1.0.0: %s", err)
		}
		addVdrTx, ok := tx.Tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			continue
		}

		nodeID := addVdrTx.Validator.ID()
		uptimeV100, err := um.previousVersionGetUptime(dbV100.Database, nodeID)
		if err == database.ErrNotFound {
			continue
		} else if err != nil {
			return fmt.Errorf("couldn't get uptime for node %s from database v1.0.0: %s", nodeID.PrefixedString(constants.NodeIDPrefix), err)
		}

		// only migrate a validator's uptime if the validator is still in the validator set.
		if _, _, err := cs.GetStaker(addVdrTx.ID()); err == database.ErrNotFound {
			// This validator isn't in the current validator set. They must have left the validator set. Ignore.
			continue
		} else if err != nil {
			return fmt.Errorf("couldn't get add validator tx %s for %s: %s", addVdrTx.ID(), nodeID.PrefixedString(constants.NodeIDPrefix), err)
		}

		// In v1.0.0, up duration is stored in seconds. In v1.4.4, it is stored in nanoseconds.
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
			"migrating uptime for node %s (tx %s) from database v1.0.0 to v1.4.4. Uptime: %s. Last updated: %s",
			nodeID.PrefixedString(constants.NodeIDPrefix),
			addVdrTx.ID(),
			time.Duration(upDuration),
			lastUpdated,
		)
		if err := um.vm.internalState.SetUptime(nodeID, time.Duration(upDuration), lastUpdated); err != nil {
			return fmt.Errorf("couldn't migrate uptime for node %s: %s", nodeID.PrefixedString(constants.NodeIDPrefix), err)
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
	um.vm.ctx.Log.Info("finished migrating platformvm from database v1.0.0 to v1.4.4")
	return nil
}

func (um *uptimeMigrater1_4_4) previousVersionGetUptime(db database.Database, nodeID ids.ShortID) (*uptimeV100, error) {
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

type uptimeV100 struct {
	UpDuration  uint64 `serialize:"true"` // In seconds
	LastUpdated uint64 `serialize:"true"` // Unix time in seconds
}

type rewardTxV100 struct {
	Reward uint64 `serialize:"true"`
	Tx     Tx     `serialize:"true"`
}
