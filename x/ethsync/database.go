// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethsync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ethereum/go-ethereum/ethdb"
)

var (
	_ database.Database = &Database{}
)

type Database struct{ ethdb.Database }

func (db Database) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}

func (db Database) NewBatch() database.Batch {
	return batch{db.Database.NewBatch()}
}

func (db Database) NewIterator() database.Iterator {
	return db.Database.NewIterator(nil, nil)
}

func (db Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.Database.NewIterator(prefix, nil)
}

func (db Database) NewIteratorWithStart(start []byte) database.Iterator {
	return db.Database.NewIterator(nil, start)
}

func (db Database) NewIteratorWithStartAndPrefix(prefix, start []byte) database.Iterator {
	return db.Database.NewIterator(prefix, start)
}

type batch struct{ ethdb.Batch }

func (b batch) Inner() database.Batch { return b }

func (b batch) Replay(w database.KeyValueWriterDeleter) error {
	return b.Batch.Replay(w)
}

func (b batch) Size() int {
	return b.Batch.ValueSize()
}
