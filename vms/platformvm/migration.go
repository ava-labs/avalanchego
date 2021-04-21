// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

func (vm *VM) migrate() error {
	prevDB, prevDBExists := vm.dbManager.Previous()
	if !prevDBExists { // there is nothing to migrate
		return nil
	}
	prevDBVersion := prevDB.Version
	currentDB := vm.dbManager.Current()
	currentDBVersion := currentDB.Version
	// Only valid migration is from database version 1.0.0 to 1.3.3
	if prevDBVersion.Compare(version.NewDefaultVersion(1, 0, 0)) == 0 &&
		currentDBVersion.Compare(version.NewDefaultVersion(1, 3, 3)) == 0 {
		return vm.migrate110(prevDB, currentDB)
	}
	return nil
}

func (vm *VM) migrate110(prevDB, currentDB *manager.VersionedDatabase) error {
	migrated, err := vm.DB.Has(migratedKey)
	if err != nil {
		return err
	} else if migrated { // already did migration
		return nil
	}

	vm.Ctx.Log.Info("migrating platformvm from database version %s to %s", prevDB.Version, currentDB.Version)
	stopPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, vm.DB)
	defer stopDB.Close()

	stopIter := stopDB.NewIterator()
	defer stopIter.Release()

	for stopIter.Next() { // Iterates in order of increasing stop time
		txBytes := stopIter.Value()
		tx := rewardTx{}
		if _, err := vm.codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Tx.Sign(vm.codec, nil); err != nil {
			return err
		}

		unsignedTx, ok := tx.Tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			continue
		}

		nodeID := unsignedTx.Validator.ID()
		uptime, err := vm.uptime(prevDB, nodeID)
		switch {
		case err == database.ErrNotFound:
			vm.Ctx.Log.Debug("Couldn't find uptime in prior database for %s: %s", nodeID, err)
			uptime = &validatorUptime{
				LastUpdated: uint64(unsignedTx.StartTime().Unix()),
			}
		case err != nil:
			return err
		}

		lastUpdated := time.Unix(int64(uptime.LastUpdated), 0)
		if !vm.bootstrappedTime.After(lastUpdated) {
			vm.Ctx.Log.Warn("bootstrapped time (%s) at or before lastUpdated (%s)", vm.bootstrappedTime, lastUpdated)
			continue
		}
		durationOffline := vm.bootstrappedTime.Sub(lastUpdated)
		uptime.UpDuration += uint64(durationOffline / time.Second)
		uptime.LastUpdated = uint64(vm.bootstrappedTime.Unix())
		if err := vm.setUptime(vm.DB, nodeID, uptime); err != nil {
			return err
		}
	}
	if err := stopIter.Error(); err != nil {
		return err
	}

	errs := wrappers.Errs{}
	errs.Add(
		vm.DB.Put(migratedKey, []byte(prevDB.Version.String())),
		vm.DB.Commit(),
		stopDB.Close(),
	)
	if errs.Err != nil {
		return err
	}

	vm.Ctx.Log.Info("finished migrating platformvm from database version %s to %s", prevDB.Version, currentDB.Version)
	return nil
}
