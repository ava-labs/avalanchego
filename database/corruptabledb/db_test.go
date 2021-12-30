// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corruptabledb

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/stretchr/testify/assert"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db := New(baseDB)
		test(t, db)
	}
}

// TestCorruption tests to make sure corruptabledb wrapper works as expected.
func TestCorruption(t *testing.T) {
	key := []byte("hello")
	value := []byte("world")
	tests := map[string]func(db database.Database) error{
		"corrupted has": func(db database.Database) error {
			_, err := db.Has(key)
			return err
		},
		"corrupted get": func(db database.Database) error {
			_, err := db.Get(key)
			return err
		},
		"corrupted put": func(db database.Database) error {
			return db.Put(key, value)
		},
		"corrupted delete": func(db database.Database) error {
			return db.Delete(key)
		},
		"corrupted batch": func(db database.Database) error {
			corruptableBatch := db.NewBatch()
			assert.NotNil(t, corruptableBatch)

			err := corruptableBatch.Put(key, value)
			assert.NoError(t, err)

			return corruptableBatch.Write()
		},
	}
	baseDB := memdb.New()
	// wrap this db
	corruptableDB := New(baseDB)
	initError := errors.New("corruption error")
	_ = corruptableDB.handleError(initError)
	for name, testFn := range tests {
		t.Run(name, func(tt *testing.T) {
			err := testFn(corruptableDB)
			assert.ErrorIsf(tt, err, initError, "not received the corruption error")
		})
	}
}
