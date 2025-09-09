// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/core/state/snapshot"
	"github.com/ava-labs/subnet-evm/triedb/hashdb"
)

const (
	namespace = "chain"

	// triePrefetchMetricsPrefix is the prefix for trie prefetcher metrics
	// and MUST match upstream for the benchmark to work.
	triePrefetchMetricsPrefix = "trie/prefetch/"
)

// BenchmarkPrefetcherDatabase benchmarks the performance of the trie
// prefetcher. By default, a state with 100k storage keys is created and stored
// in a temporary directory. Setting the TEST_DB_KVS and TEST_DB_DIR environment
// variables modifies the defaults. The benchmark measures the time to update
// the trie after 100, 200, and 500 storage slot updates per iteration,
// simulating a block with that number of storage slot updates. For performance
// reasons, when making changes involving the trie prefetcher, this benchmark
// should be run against a state including around 100m storage entries.
func BenchmarkPrefetcherDatabase(b *testing.B) {
	require := require.New(b)

	dir := b.TempDir()
	if env := os.Getenv("TEST_DB_DIR"); env != "" {
		dir = env
	}
	wantKVs := 100_000
	if env := os.Getenv("TEST_DB_KVS"); env != "" {
		var err error
		wantKVs, err = strconv.Atoi(env)
		require.NoError(err)
	}

	levelDB, err := rawdb.NewLevelDBDatabase(path.Join(dir, "level.db"), 0, 0, "", false)
	require.NoError(err)

	root := types.EmptyRootHash
	count := uint64(0)
	block := uint64(0)

	rootKey := []byte("root")
	countKey := []byte("count")
	blockKey := []byte("block")
	got, err := levelDB.Get(rootKey)
	if err == nil { // on success
		root = common.BytesToHash(got)
	}
	got, err = levelDB.Get(countKey)
	if err == nil { // on success
		count = binary.BigEndian.Uint64(got)
	}
	got, err = levelDB.Get(blockKey)
	if err == nil { // on success
		block = binary.BigEndian.Uint64(got)
	}

	// Make a trie on the levelDB
	address1 := common.Address{42}
	address2 := common.Address{43}
	addBlock := func(db state.Database, snaps *snapshot.Tree, kvsPerBlock int, prefetchers int) {
		_, root, err = addKVs(db, snaps, address1, address2, root, block, kvsPerBlock, prefetchers)
		require.NoError(err)
		count += uint64(kvsPerBlock)
		block++
	}

	lastCommit := block
	commit := func(levelDB ethdb.Database, snaps *snapshot.Tree, db state.Database) {
		require.NoError(db.TrieDB().Commit(root, false))

		for i := lastCommit + 1; i <= block; i++ {
			require.NoError(snaps.Flatten(fakeHash(i)))
		}
		lastCommit = block

		// update the tracking keys
		require.NoError(levelDB.Put(rootKey, root.Bytes()))
		require.NoError(database.PutUInt64(levelDB, blockKey, block))
		require.NoError(database.PutUInt64(levelDB, countKey, count))
	}

	tdbConfig := &triedb.Config{
		DBOverride: hashdb.Config{
			CleanCacheSize: 3 * 1024 * 1024 * 1024,
		}.BackendConstructor,
	}
	db := state.NewDatabaseWithConfig(levelDB, tdbConfig)
	snaps := snapshot.NewTestTree(levelDB, fakeHash(block), root)
	for count < uint64(wantKVs) {
		previous := root
		addBlock(db, snaps, 100_000, 0) // Note this updates root and count
		b.Logf("Root: %v, kvs: %d, block: %d", root, count, block)

		// Commit every 10 blocks or on the last iteration
		if block%10 == 0 || count >= uint64(wantKVs) {
			commit(levelDB, snaps, db)
			b.Logf("Root: %v, kvs: %d, block: %d (committed)", root, count, block)
		}
		if previous != root {
			require.NoError(db.TrieDB().Dereference(previous))
		} else {
			b.Fatal("root did not change")
		}
	}
	require.NoError(levelDB.Close())
	b.Log("Starting benchmarks")
	b.Logf("Root: %v, kvs: %d, block: %d", root, count, block)
	for _, updates := range []int{100, 200, 500} {
		for _, prefetchers := range []int{0, 1, 4, 16} {
			b.Run(fmt.Sprintf("updates_%d_prefetchers_%d", updates, prefetchers), func(b *testing.B) {
				startRoot, startBlock, startCount := root, block, count
				defer func() { root, block, count = startRoot, startBlock, startCount }()

				levelDB, err := rawdb.NewLevelDBDatabase(path.Join(dir, "level.db"), 0, 0, "", false)
				require.NoError(err)
				snaps := snapshot.NewTestTree(levelDB, fakeHash(block), root)
				db := state.NewDatabaseWithConfig(levelDB, tdbConfig)
				getMetric := func(metric string) int64 {
					meter := metrics.GetOrRegisterMeter(triePrefetchMetricsPrefix+namespace+"/storage/"+metric, nil)
					return meter.Snapshot().Count()
				}
				startLoads := getMetric("load")
				for i := 0; i < b.N; i++ {
					addBlock(db, snaps, updates, prefetchers)
				}
				require.NoError(levelDB.Close())
				b.ReportMetric(float64(getMetric("load")-startLoads)/float64(b.N), "loads")
			})
		}
	}
}

func fakeHash(block uint64) common.Hash {
	return common.BytesToHash(binary.BigEndian.AppendUint64(nil, block))
}

// addKVs adds count random key-value pairs to the state trie of address1 and
// address2 (each count/2) and returns the new state db and root.
func addKVs(
	db state.Database, snaps *snapshot.Tree,
	address1, address2 common.Address, root common.Hash, block uint64,
	count int, prefetchers int,
) (*state.StateDB, common.Hash, error) {
	statedb, err := state.New(root, db, snaps)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("creating state with snapshot: %w", err)
	}
	if prefetchers > 0 {
		statedb.StartPrefetcher(namespace, extstate.WithConcurrentWorkers(prefetchers))
		defer statedb.StopPrefetcher()
	}
	for _, address := range []common.Address{address1, address2} {
		statedb.SetNonce(address, 1)
		for i := 0; i < count/2; i++ {
			key := make([]byte, 32)
			value := make([]byte, 32)
			rand.Read(key)
			rand.Read(value)

			statedb.SetState(address, common.BytesToHash(key), common.BytesToHash(value))
		}
	}
	snapshotOpt := snapshot.WithBlockHashes(fakeHash(block+1), fakeHash(block))
	root, err = statedb.Commit(block+1, true, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("committing with snap: %w", err)
	}
	return statedb, root, nil
}
