// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"math/rand"
	"os"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Note: TestInterface tests other batch functionality.
func TestBatch(t *testing.T) {
	require := require.New(t)
	dirName := os.TempDir()
	defer os.Remove(dirName)

	db, err := New(dirName, DefaultConfig, logging.NoLog{}, "", prometheus.NewRegistry())
	require.NoError(err)

	batchIntf := db.NewBatch()
	batch, ok := batchIntf.(*batch)
	require.True(ok)

	require.False(batch.written.Load())

	key1, value1 := []byte("key1"), []byte("value1")
	require.NoError(batch.Put(key1, value1))
	require.Equal(len(key1)+len(value1)+pebbleByteOverHead, batch.Size())

	require.NoError(batch.Write())

	require.True(batch.written.Load())

	got, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, got)

	batch.Reset()
	require.False(batch.written.Load())
	require.Zero(batch.Size())
}

func FuzzConcurrenctBatchWrite(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		seed int64,
		numOps int,
	) {
		require := require.New(t)
		dirName, err := os.MkdirTemp("", "TestBatch-*")
		require.NoError(err)

		defer os.Remove(dirName)

		db, err := New(dirName, DefaultConfig, logging.NoLog{}, "", prometheus.NewRegistry())
		require.NoError(err)

		r := rand.New(rand.NewSource(seed))

		batch1 := db.NewBatch()
		batch2 := db.NewBatch()
		for i := 0; i < numOps; i++ {
			key := make([]byte, r.Intn(16))
			_, _ = r.Read(key)
			value := make([]byte, r.Intn(16))
			_, _ = r.Read(value)
			require.NoError(batch1.Put(key, value))

			_, _ = r.Read(key)
			_, _ = r.Read(value)
			require.NoError(batch2.Put(key, value))
		}

		wg := sync.WaitGroup{}
		wg.Add(2)

		go func() {
			defer wg.Done()
			require.NoError(batch1.Write())
		}()

		go func() {
			defer wg.Done()
			require.NoError(batch2.Write())
		}()

		wg.Wait()
	})
}
