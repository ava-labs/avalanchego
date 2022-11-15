// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package encdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

const testPassword = "lol totally a secure password" //nolint:gosec

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		unencryptedDB := memdb.New()
		db, err := New([]byte(testPassword), unencryptedDB)
		if err != nil {
			t.Fatal(err)
		}

		test(t, db)
	}
}

func FuzzInterface(f *testing.F) {
	for _, test := range database.FuzzTests {
		unencryptedDB := memdb.New()
		db, err := New([]byte(testPassword), unencryptedDB)
		if err != nil {
			require.NoError(f, err)
		}
		test(f, db)
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			unencryptedDB := memdb.New()
			db, err := New([]byte(testPassword), unencryptedDB)
			if err != nil {
				b.Fatal(err)
			}
			bench(b, db, "encdb", keys, values)
		}
	}
}
