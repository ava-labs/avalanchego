// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versionabledb

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db := New(baseDB)
		test(t, db)
	}
}

func TestInterfaceCommitBatch(t *testing.T) {
	nonClosingTests := []func(t *testing.T, db database.Database){
		database.TestSimpleKeyValue,
		database.TestBatchDelete,
		database.TestBatchReset,
		database.TestBatchReuse,
		database.TestBatchRewrite,
		database.TestBatchInner,
		database.TestIterator,
		database.TestIteratorStart,
		database.TestIteratorPrefix,
		database.TestIteratorStartPrefix,
		database.TestIteratorMemorySafety,
		database.TestMemorySafetyDatabase,
		database.TestMemorySafetyBatch,
	}
	for _, test := range nonClosingTests {
		baseDB := memdb.New()
		db := New(baseDB)
		db.StartCommit()
		test(t, db)
		batch, err := db.CommitBatch()
		if err != nil {
			t.Fatalf("Unexpected error on db.CommitBatch: %s", err)
		}
		if err := batch.Write(); err != nil {
			t.Fatalf("Unexpected error on batch.Write: %s", err)
		}
		db.EndBatch()
	}
}

func TestInterfaceAbort(t *testing.T) {
	for _, test := range database.Tests {
		baseDB := memdb.New()
		db := New(baseDB)
		db.StartCommit()
		test(t, db)
		db.AbortCommit()
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size, size)
		for _, bench := range database.Benchmarks {
			baseDB := memdb.New()
			db := New(baseDB)
			bench(b, db, "versionabledb", keys, values)
		}
	}
}
