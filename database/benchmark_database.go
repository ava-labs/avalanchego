// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"fmt"
	"math/rand"
	"testing"
)

var (
	// Benchmarks is a list of all database benchmarks
	Benchmarks = []func(b *testing.B, db Database, name string, keys, values [][]byte){
		BenchmarkGet,
		BenchmarkPut,
		BenchmarkDelete,
		BenchmarkBatchPut,
		BenchmarkParallelGet,
		BenchmarkParallelPut,
		BenchmarkParallelDelete,
	}
	// BenchmarkSizes to use with each benchmark
	BenchmarkSizes = []int{
		32,
		256,
		2048,
	}
)

// Writes size data into the db in order to setup reads in subsequent tests.
func SetupBenchmark(b *testing.B, count int, size int) ([][]byte, [][]byte) {
	b.Helper()

	keys := make([][]byte, count)
	values := make([][]byte, count)
	for i := 0; i < count; i++ {
		keyBytes := make([]byte, size)
		valueBytes := make([]byte, size)
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

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_get", name, count, len(keys[0])), func(b *testing.B) {
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

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_put", name, count, len(keys[0])), func(b *testing.B) {
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

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_delete", name, count, len(keys[0])), func(b *testing.B) {
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

// BenchmarkBatchPut measures the time it takes to batch write.
//nolint:interfacer // This function takes in a database to be the expected type.
func BenchmarkBatchPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_write_batch", name, count, len(keys[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i := 0; i < b.N; i++ {
			if err := batch.Put(keys[i%count], values[i%count]); err != nil {
				b.Fatalf("Unexpected error in db.Put: %s", err)
			}
		}
		if err := batch.Write(); err != nil {
			b.Fatalf("Unexpected error in batch.Write: %s", err)
		}
	})
}

// BenchmarkParallelGet measures the time it takes to read in parallel.
func BenchmarkParallelGet(b *testing.B, db Database, name string, keys, values [][]byte) {
	count := len(keys)
	if count == 0 {
		b.Fatal("no keys")
	}

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_get_par", name, count, len(keys[0])), func(b *testing.B) {
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

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_write_par", name, count, len(keys[0])), func(b *testing.B) {
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

	b.Run(fmt.Sprintf("%s_%d_values_%d_bytes_delete_par", name, count, len(keys[0])), func(b *testing.B) {
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
