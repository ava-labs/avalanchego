// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/units"
)

var (
	// Benchmarks is a list of all database benchmarks
	Benchmarks = []func(b *testing.B, db Database, name string, keys, values [][]byte){
		BenchmarkGet,
		BenchmarkPut,
		BenchmarkDelete,
		BenchmarkBatchPut,
		BenchmarkBatchDelete,
		BenchmarkBatchWrite,
		BenchmarkParallelGet,
		BenchmarkParallelPut,
		BenchmarkParallelDelete,
	}
	// BenchmarkSizes to use with each benchmark
	BenchmarkSizes = [][]int{
		// count, keySize, valueSize
		{1024, 32, 32},
		{1024, 256, 256},
		{1024, 2 * units.KiB, 2 * units.KiB},
	}
)

// Writes size data into the db in order to setup reads in subsequent tests.
func SetupBenchmark(b *testing.B, count int, keySize, valueSize int) ([][]byte, [][]byte) {
	b.Helper()

	keys := make([][]byte, count)
	values := make([][]byte, count)
	for i := 0; i < count; i++ {
		keyBytes := make([]byte, keySize)
		valueBytes := make([]byte, valueSize)
		_, err := rand.Read(keyBytes) // #nosec G404
		if err != nil {
			b.Fatal(err)
		}
		_, err = rand.Read(valueBytes) // #nosec G404
		if err != nil {
			b.Fatal(err)
		}
		keys[i], values[i] = keyBytes, valueBytes
	}
	return keys, values
}

// BenchmarkGet measures the time it takes to get an operation from a database.
func BenchmarkGet(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.get", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		for i, key := range keys {
			value := values[i]
			if err := db.Put(key, value); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}

		b.ResetTimer()

		// Reads b.N values from the db
		for i := 0; i < b.N; i++ {
			if _, err := db.Get(keys[i%count]); err != nil {
				b.Fatalf("Unexpected error in Get %s", err)
			}
		}
	})
}

// BenchmarkPut measures the time it takes to write an operation to a database.
func BenchmarkPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.put", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		// Writes b.N values to the db
		for i := 0; i < b.N; i++ {
			if err := db.Put(keys[i%count], values[i%count]); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}
	})
}

// BenchmarkDelete measures the time it takes to delete a (k, v) from a database.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.delete", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		// Writes random values of size _size_ to the database
		for i, key := range keys {
			value := values[i]
			if err := db.Put(key, value); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}

		b.ResetTimer()

		// Deletes b.N values from the db
		for i := 0; i < b.N; i++ {
			if err := db.Delete(keys[i%count]); err != nil {
				b.Fatalf("Unexpected error in Delete %s", err)
			}
		}
	})
}

// BenchmarkBatchPut measures the time it takes to batch put.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkBatchPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.put", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i := 0; i < b.N; i++ {
			if err := batch.Put(keys[i%count], values[i%count]); err != nil {
				b.Fatalf("Unexpected error in batch.Put: %s", err)
			}
		}
	})
}

// BenchmarkBatchDelete measures the time it takes to batch delete.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkBatchDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.delete", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i := 0; i < b.N; i++ {
			if err := batch.Delete(keys[i%count]); err != nil {
				b.Fatalf("Unexpected error in batch.Delete: %s", err)
			}
		}
	})
}

// BenchmarkBatchWrite measures the time it takes to batch write.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkBatchWrite(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.write", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i, key := range keys {
			value := values[i]

			if err := batch.Put(key, value); err != nil {
				b.Fatalf("Unexpected error in batch.Put: %s", err)
			}
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if err := batch.Write(); err != nil {
				b.Fatalf("Unexpected error in batch.Write: %s", err)
			}
		}
	})
}

// BenchmarkParallelGet measures the time it takes to read in parallel.
func BenchmarkParallelGet(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.get_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		for i, key := range keys {
			value := values[i]
			if err := db.Put(key, value); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}

		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for i := 0; pb.Next(); i++ {
				if _, err := db.Get(keys[i%count]); err != nil {
					b.Fatalf("Unexpected error in Get %s", err)
				}
			}
		})
	})
}

// BenchmarkParallelPut measures the time it takes to write to the db in parallel.
func BenchmarkParallelPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.put_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			// Write N values to the db
			for i := 0; pb.Next(); i++ {
				if err := db.Put(keys[i%count], values[i%count]); err != nil {
					b.Fatalf("Unexpected error in Put %s", err)
				}
			}
		})
	})
}

// BenchmarkParallelDelete measures the time it takes to delete a (k, v) from the db.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkParallelDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.delete_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		for i, key := range keys {
			value := values[i]
			if err := db.Put(key, value); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			// Deletes b.N values from the db
			for i := 0; pb.Next(); i++ {
				if err := db.Delete(keys[i%count]); err != nil {
					b.Fatalf("Unexpected error in Delete %s", err)
				}
			}
		})
	})
}
