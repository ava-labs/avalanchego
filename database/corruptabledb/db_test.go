// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corruptabledb

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/databasemock"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var errTest = errors.New("non-nil error")

func newDB() *Database {
	baseDB := memdb.New()
	return New(baseDB, logging.NoLog{})
}

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			test(t, newDB())
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	dbtest.FuzzKeyValue(f, newDB())
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithPrefix(f, newDB())
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, newDB())
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

			require.NoError(t, corruptableBatch.Put(key, value))

			return corruptableBatch.Write()
		},
		"corrupted healthcheck": func(db database.Database) error {
			_, err := db.HealthCheck(context.Background())
			return err
		},
	}
	corruptableDB := newDB()
	_ = corruptableDB.handleError(errTest)
	for name, testFn := range tests {
		t.Run(name, func(tt *testing.T) {
			err := testFn(corruptableDB)
			require.ErrorIs(tt, err, errTest)
		})
	}
}

func TestIterator(t *testing.T) {
	errIter := errors.New("iterator error")

	type test struct {
		name              string
		databaseErrBefore error
		modifyIter        func(*gomock.Controller, *iterator)
		op                func(*require.Assertions, *iterator)
		expectedErr       error
	}

	tests := []test{
		{
			name:              "corrupted database; Next",
			databaseErrBefore: errTest,
			expectedErr:       errTest,
			modifyIter:        func(*gomock.Controller, *iterator) {},
			op: func(require *require.Assertions, iter *iterator) {
				require.False(iter.Next())
			},
		},
		{
			name:              "Next corrupts database",
			databaseErrBefore: nil,
			expectedErr:       errIter,
			modifyIter: func(ctrl *gomock.Controller, iter *iterator) {
				mockInnerIter := databasemock.NewIterator(ctrl)
				mockInnerIter.EXPECT().Next().Return(false)
				mockInnerIter.EXPECT().Error().Return(errIter)
				iter.Iterator = mockInnerIter
			},
			op: func(require *require.Assertions, iter *iterator) {
				require.False(iter.Next())
			},
		},
		{
			name:              "corrupted database; Error",
			databaseErrBefore: errTest,
			expectedErr:       errTest,
			modifyIter:        func(*gomock.Controller, *iterator) {},
			op: func(require *require.Assertions, iter *iterator) {
				err := iter.Error()
				require.ErrorIs(err, errTest)
			},
		},
		{
			name:              "Error corrupts database",
			databaseErrBefore: nil,
			expectedErr:       errIter,
			modifyIter: func(ctrl *gomock.Controller, iter *iterator) {
				mockInnerIter := databasemock.NewIterator(ctrl)
				mockInnerIter.EXPECT().Error().Return(errIter)
				iter.Iterator = mockInnerIter
			},
			op: func(require *require.Assertions, iter *iterator) {
				err := iter.Error()
				require.ErrorIs(err, errIter)
			},
		},
		{
			name:              "corrupted database; Key",
			databaseErrBefore: errTest,
			expectedErr:       errTest,
			modifyIter:        func(*gomock.Controller, *iterator) {},
			op: func(_ *require.Assertions, iter *iterator) {
				_ = iter.Key()
			},
		},
		{
			name:              "corrupted database; Value",
			databaseErrBefore: errTest,
			expectedErr:       errTest,
			modifyIter:        func(*gomock.Controller, *iterator) {},
			op: func(_ *require.Assertions, iter *iterator) {
				_ = iter.Value()
			},
		},
		{
			name:              "corrupted database; Release",
			databaseErrBefore: errTest,
			expectedErr:       errTest,
			modifyIter:        func(*gomock.Controller, *iterator) {},
			op: func(_ *require.Assertions, iter *iterator) {
				iter.Release()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			// Make a database
			corruptableDB := newDB()
			// Put a key-value pair in the database.
			require.NoError(corruptableDB.Put([]byte{0}, []byte{1}))

			// Mark database as corrupted, if applicable
			_ = corruptableDB.handleError(tt.databaseErrBefore)

			// Make an iterator
			iter := &iterator{
				Iterator: corruptableDB.NewIterator(),
				db:       corruptableDB,
			}

			// Modify the iterator (optional)
			tt.modifyIter(ctrl, iter)

			// Do an iterator operation
			tt.op(require, iter)

			err := corruptableDB.corrupted()
			require.ErrorIs(err, tt.expectedErr)
		})
	}
}
