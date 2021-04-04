// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package keystore

import (
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

func (ks *Keystore) Migrate() error {
	latestDBVersion := ks.dbManager.Current().Version.String()
	_, prevDBExists := ks.dbManager.Previous()
	if !prevDBExists {
		return nil
	}

	switch latestDBVersion {
	case "v1.1.0":
		return ks.migrate110()
	}
	return nil
}

func (ks *Keystore) migrate110() error {
	currentDB := ks.dbManager.Current()
	previousDB, exists := ks.dbManager.Previous()
	if !exists {
		return nil
	}

	migrated, err := currentDB.Has(migratedKey)
	if err != nil {
		return err
	}
	// If the currentDB has already been marked as migrated
	// then skip migrating the keystore users.
	if migrated {
		return nil
	}

	previousUserDB := prefixdb.New(usersPrefix, previousDB)
	previousBCDB := prefixdb.New(bcsPrefix, previousDB)

	ks.log.Info("Migrating Keystore Users from %s -> %s", previousDB.Version, currentDB.Version)

	userIterator := previousUserDB.NewIterator()
	defer userIterator.Release()

	for userIterator.Next() {
		username := userIterator.Key()

		exists, err := ks.userDB.Has(username)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		userBatch := ks.userDB.NewBatch()
		if err := userBatch.Put(username, userIterator.Value()); err != nil {
			return err
		}

		currentUserBCDB := prefixdb.New(username, ks.bcDB)
		previousUserBCDB := prefixdb.New(username, previousBCDB)

		bcsBatch := currentUserBCDB.NewBatch()

		if err := ks.migrateUserBCDB(previousUserBCDB, bcsBatch, userBatch); err != nil {
			return err
		}
	}

	if err := userIterator.Error(); err != nil {
		return err
	}

	return currentDB.Put(migratedKey, []byte(previousDB.Version.String()))
}
