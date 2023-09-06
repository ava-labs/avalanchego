// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

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
	require := require.New(b)

	b.Helper()

	keys := make([][]byte, count)
	values := make([][]byte, count)
	for i := 0; i < count; i++ {
		keyBytes := make([]byte, keySize)
		valueBytes := make([]byte, valueSize)
		_, err := rand.Read(keyBytes) // #nosec G404
		require.NoError(err)
		_, err = rand.Read(valueBytes) // #nosec G404
		require.NoError(err)
		keys[i], values[i] = keyBytes, valueBytes
	}
	return keys, values
}

// BenchmarkGet measures the time it takes to get an operation from a database.
func BenchmarkGet(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.get", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		require := require.New(b)

		for i, key := range keys {
			value := values[i]
			require.NoError(db.Put(key, value))
		}

		b.ResetTimer()

		// Reads b.N values from the db
		for i := 0; i < b.N; i++ {
			_, err := db.Get(keys[i%count])
			require.NoError(err)
		}
	})
}

// BenchmarkPut measures the time it takes to write an operation to a database.
func BenchmarkPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.put", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		// Writes b.N values to the db
		for i := 0; i < b.N; i++ {
			require.NoError(b, db.Put(keys[i%count], values[i%count]))
		}
	})
}

// BenchmarkDelete measures the time it takes to delete a (k, v) from a database.
func BenchmarkDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.delete", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		require := require.New(b)

		// Writes random values of size _size_ to the database
		for i, key := range keys {
			value := values[i]
			require.NoError(db.Put(key, value))
		}

		b.ResetTimer()

		// Deletes b.N values from the db
		for i := 0; i < b.N; i++ {
			require.NoError(db.Delete(keys[i%count]))
		}
	})
}

// BenchmarkBatchPut measures the time it takes to batch put.
func BenchmarkBatchPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.put", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i := 0; i < b.N; i++ {
			require.NoError(b, batch.Put(keys[i%count], values[i%count]))
		}
	})
}

// BenchmarkBatchDelete measures the time it takes to batch delete.
func BenchmarkBatchDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.delete", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		batch := db.NewBatch()
		for i := 0; i < b.N; i++ {
			require.NoError(b, batch.Delete(keys[i%count]))
		}
	})
}

// BenchmarkBatchWrite measures the time it takes to batch write.
func BenchmarkBatchWrite(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_batch.write", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		require := require.New(b)

		batch := db.NewBatch()
		for i, key := range keys {
			value := values[i]
			require.NoError(batch.Put(key, value))
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			require.NoError(batch.Write())
		}
	})
}

// BenchmarkParallelGet measures the time it takes to read in parallel.
func BenchmarkParallelGet(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.get_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		require := require.New(b)

		for i, key := range keys {
			value := values[i]
			require.NoError(db.Put(key, value))
		}

		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for i := 0; pb.Next(); i++ {
				_, err := db.Get(keys[i%count])
				require.NoError(err)
			}
		})
	})
}

// BenchmarkParallelPut measures the time it takes to write to the db in parallel.
func BenchmarkParallelPut(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.put_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			// Write N values to the db
			for i := 0; pb.Next(); i++ {
				require.NoError(b, db.Put(keys[i%count], values[i%count]))
			}
		})
	})
}

// BenchmarkParallelDelete measures the time it takes to delete a (k, v) from the db.
func BenchmarkParallelDelete(b *testing.B, db Database, name string, keys, values [][]byte) {
	require.NotEmpty(b, keys)
	count := len(keys)

	b.Run(fmt.Sprintf("%s_%d_pairs_%d_keys_%d_values_db.delete_parallel", name, count, len(keys[0]), len(values[0])), func(b *testing.B) {
		require := require.New(b)
		for i, key := range keys {
			value := values[i]
			require.NoError(db.Put(key, value))
		}
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			// Deletes b.N values from the db
			for i := 0; pb.Next(); i++ {
				require.NoError(db.Delete(keys[i%count]))
			}
		})
	})
}
