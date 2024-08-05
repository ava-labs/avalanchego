// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package encdb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

const testPassword = "lol totally a secure password" //nolint:gosec

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			unencryptedDB := memdb.New()
			db, err := New([]byte(testPassword), unencryptedDB)
			require.NoError(t, err)

			test(t, db)
		})
	}
}

func newDB(t testing.TB) database.Database {
	unencryptedDB := memdb.New()
	db, err := New([]byte(testPassword), unencryptedDB)
	require.NoError(t, err)
	return db
}

func FuzzKeyValue(f *testing.F) {
	dbtest.FuzzKeyValue(f, newDB(f))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithPrefix(f, newDB(f))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, newDB(f))
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("encdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				bench(b, newDB(b), keys, values)
			})
		}
	}
}
