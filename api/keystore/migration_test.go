// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
)

func TestMigrationNoPrevDB(t *testing.T) {
	assert := assert.New(t)

	dbManager, err := manager.NewManagerFromDBs([]*manager.VersionedDatabase{
		{
			Database: memdb.New(),
			Version:  version.DefaultVersion1_0_0,
		},
	})
	assert.NoError(err)

	_, err = New(logging.NoLog{}, dbManager)
	assert.NoError(err)
}

// Test migration from database version 1.0.0 to 1.4.4
func TestMigration1_0_0To1_4_4(t *testing.T) {
	assert := assert.New(t)

	username := "bob"

	key := []byte{1}
	value := []byte{2}

	DatabaseVersion1_0_0 := memdb.New()

	dbManagerV1_0_0, err := manager.NewManagerFromDBs([]*manager.VersionedDatabase{
		{
			Database: prefixdb.New(nil, DatabaseVersion1_0_0),
			Version:  version.DefaultVersion1_0_0,
		},
	})
	assert.NoError(err)

	ksV1_0_0, err := New(&logging.NoLog{}, dbManagerV1_0_0)
	assert.NoError(err)

	err = ksV1_0_0.CreateUser(username, strongPassword)
	assert.NoError(err)

	userDatabaseVersion1_0_0, err := ksV1_0_0.GetDatabase(ids.Empty, username, strongPassword)
	assert.NoError(err)

	err = userDatabaseVersion1_0_0.Put(key, value)
	assert.NoError(err)

	DatabaseVersion1_4_4 := memdb.New()

	v1_4_4 := version.DatabaseVersion1_4_4

	dbManagerV1_4_4, err := manager.NewManagerFromDBs([]*manager.VersionedDatabase{
		{
			Database: prefixdb.New(nil, DatabaseVersion1_4_4),
			Version:  v1_4_4,
		},
		{
			Database: prefixdb.New(nil, DatabaseVersion1_0_0),
			Version:  version.DefaultVersion1_0_0,
		},
	})
	assert.NoError(err)

	ksV1_4_4, err := New(&logging.NoLog{}, dbManagerV1_4_4)
	assert.NoError(err)

	userDatabaseVersion1_4_4, err := ksV1_4_4.GetDatabase(ids.Empty, username, strongPassword)
	assert.NoError(err)

	val, err := userDatabaseVersion1_4_4.Get(key)
	assert.NoError(err)
	assert.Equal(value, val)
}
