// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func (vm *VM) Migrate() error {
	latestDBVersion := vm.dbManager.Current().Version.String()
	_, prevDBExists := vm.dbManager.Previous()
	if !prevDBExists {
		return nil
	}

	switch latestDBVersion {
	case "v1.1.0":
		return vm.migrate110()
	}
	return nil
}

func (vm *VM) migrate110() error {
	previousDB, _ := vm.dbManager.Previous()
	completedMigration, err := vm.DB.Has(migratedKey)
	if err != nil {
		return err
	}

	if completedMigration {
		return nil
	}

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

		uptime, err := vm.uptime(previousDB, nodeID)
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

		// todo review this
		if !vm.bootstrappedTime.After(lastUpdated) {
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
		vm.DB.Put(migratedKey, []byte(previousDB.Version.String())),
		vm.DB.Commit(),
		stopDB.Close(),
	)
	return err
}
