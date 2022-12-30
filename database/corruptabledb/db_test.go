// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corruptabledb

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

var errTest = errors.New("non-nil error")

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db := New(baseDB)
		test(t, db)
	}
}

func FuzzInterface(f *testing.F) {
	for _, test := range database.FuzzTests {
		baseDB := memdb.New()
		db := New(baseDB)
		test(f, db)
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
			require.NotNil(t, corruptableBatch)

			err := corruptableBatch.Put(key, value)
			require.NoError(t, err)

			return corruptableBatch.Write()
		},
		"corrupted healthcheck": func(db database.Database) error {
			_, err := db.HealthCheck(context.Background())
			return err
		},
	}
	baseDB := memdb.New()
	// wrap this db
	corruptableDB := New(baseDB)
	_ = corruptableDB.handleError(errTest)
	for name, testFn := range tests {
		t.Run(name, func(tt *testing.T) {
			err := testFn(corruptableDB)
			require.ErrorIsf(tt, err, errTest, "not received the corruption error")
		})
	}
}
