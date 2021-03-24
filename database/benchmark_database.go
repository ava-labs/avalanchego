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
	Benchmarks = []func(b *testing.B, db Database, name string, count int, size int){
		BenchmarkGet,
		BenchmarkPut,
		BenchmarkDelete,
		BenchmarkBatchPut,
		BenchmarkParallelGet,
		BenchmarkParallelPut,
		BenchmarkParallelDelete,
	}
)

// Writes size data into the db in order to setup reads in subsequent tests.
func benchmarkSetup(b *testing.B, count int, size int) ([][]byte, [][]byte) {
	keys := make([][]byte, count)
	values := make([][]byte, count)
	for i := 0; i < b.N; i++ {
		keyBytes := make([]byte, b.N)
		valueBytes := make([]byte, size)
		rand.Read(keyBytes)
		rand.Read(valueBytes)
		keys[i], values[i] = keyBytes, valueBytes
	}
	return keys, values
}

// BenchmarkGet measures the time it takes to get an operation from a database.
func BenchmarkGet(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_get", name, count, size), func(b *testing.B) {
		// Writes random values of size _size_ to the database
		for i := 0; i < b.N; i++ {
			if err := db.Put(keys[i%count], values[i%count]); err != nil {
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
func BenchmarkPut(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_put", name, count, size), func(b *testing.B) {
		// Writes b.N values to the db
		for i := 0; i < b.N; i++ {
			if err := db.Put(keys[i%count], values[i%count]); err != nil {
				b.Fatalf("Unexpected error in Put %s", err)
			}
		}
	})
}

// BenchmarkDelete measures the time it takes to delete a (k, v) from a database.
func BenchmarkDelete(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_delete", name, count, size), func(b *testing.B) {
		// Writes random values of size _size_ to the database
		for i := 0; i < b.N; i++ {
			if err := db.Put(keys[i%count], values[i%count]); err != nil {
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
func BenchmarkBatchPut(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_write_batch", name, count, size), func(b *testing.B) {
		batch := db.NewBatch()
		if batch == nil {
			b.Fatalf("db.NewBatch returned nil")
		}
		for i := 0; i < b.N; i++ {
			if err := batch.Put(keys[i%count], values[i%count]); err != nil {
				b.Fatalf("Unexpected error in db.Put: %s", err)
			}
			if err := batch.Write(); err != nil {
				b.Fatalf("Unexpected error in batch.Write: %s", err)
			}
		}
	})
}

// BenchmarkParallelGet measures the time it takes to read in parallel.
func BenchmarkParallelGet(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_get_par", name, count, size), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for i := 0; i < b.N; i++ {
				if err := db.Put(keys[i%count], values[i%count]); err != nil {
					b.Fatalf("Unexpected error in Put %s", err)
				}
			}
			b.ResetTimer()

			for i := 0; pb.Next(); i++ {
				if _, err := db.Get(keys[i%count]); err != nil {
					b.Fatalf("Unexpected error in Get %s", err)
				}
			}
		})
	})
}

// BenchmarkParallelPut measures the time it takes to write to the db in parallel.
func BenchmarkParallelPut(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_write_par", name, count, size), func(b *testing.B) {
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
func BenchmarkParallelDelete(b *testing.B, db Database, name string, count int, size int) {
	keys, values := benchmarkSetup(b, count, size)

	b.Run(fmt.Sprintf("%s_%d_%dbytes_delete_par", name, count, size), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			// Writes random values of dbSize _dbSize_ to the database
			for i := 0; pb.Next(); i++ {
				if err := db.Put(keys[i%count], values[i%count]); err != nil {
					b.Fatalf("Unexpected error in Put %s", err)
				}
			}
			b.ResetTimer()

			// Deletes b.N values from the db
			for i := 0; pb.Next(); i++ {
				if err := db.Delete(keys[i%count]); err != nil {
					b.Fatalf("Unexpected error in Delete %s", err)
				}
			}
		})
	})
}
