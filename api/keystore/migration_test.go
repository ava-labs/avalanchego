// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/manager/mocks"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
)

func TestMigrationNoPrevDB(t *testing.T) {
	assert := assert.New(t)
	ks, dbManager, err := CreateTestKeystore()
	assert.NoError(err)
	dbManager.On("Previous").Return(nil, false)
	assert.NoError(ks.(*keystore).migrate(dbManager))
}

// Test migration from database version 1.0.0 to 1.3.3
// Recall that the structure of the 1.0.0 and 1.3.3 keystore databases is:
//           BaseDB
//          /      \
//    UserDB        BlockchainDB
//                 /      |     \
//               Usr     Usr    Usr
//             /  |  \
//          BID  BID  BID
func TestMigration100_to_133(t *testing.T) {
	assert := assert.New(t)
	ks := &keystore{log: logging.NoLog{}}
	dbManager := &mocks.Manager{}

	// Put values in the user db and blockchain db
	prevDB := memdb.New()
	prevUserDB := prefixdb.New(usersPrefix, prevDB)

	username, userValue := []byte{'u', 's', 'e', 'r'}, []byte{1}
	assert.NoError(prevUserDB.Put(username, userValue))
	previousBCDB := prefixdb.New(bcsPrefix, prevDB)
	userDataKey, userDataValue := []byte{2}, []byte{3}
	userPrevBCDB := prefixdb.New(username, previousBCDB)
	assert.NoError(userPrevBCDB.Put(userDataKey, userDataValue))

	dbManager.On("Previous").Return(
		manager.NewVersionedDatabase(prevDB, version.NewDefaultVersion(1, 0, 0)),
		true,
	)

	currentDB := &manager.VersionedDatabase{
		Database: memdb.New(),
		Version:  version.NewDefaultVersion(1, 3, 3),
	}
	dbManager.On("Current").Return(currentDB)

	// Do the migration
	assert.NoError(ks.initializeDB(dbManager))

	// Make sure the data was migrated
	userDB := prefixdb.New(usersPrefix, currentDB.Database)
	gotPrevUserDBVal, err := userDB.Get(username)
	assert.NoError(err)
	assert.EqualValues(userValue, gotPrevUserDBVal)

	bcDB := prefixdb.New(bcsPrefix, currentDB.Database)
	userBCDB := prefixdb.New(username, bcDB)
	gotPrevBCDBVal, err := userBCDB.Get(userDataKey)
	assert.NoError(err)
	assert.EqualValues(userDataValue, gotPrevBCDBVal)
}
