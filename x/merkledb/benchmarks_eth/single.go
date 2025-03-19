// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
)

func runSingleBenchmark(databaseEntries uint64) error {
	rootBytes, err := os.ReadFile(path.Join(getRunningDatabaseDirectory(databaseEntries), "root.txt"))
	if err != nil {
		return fmt.Errorf("unable to read root : %v", err)
	}

	ldb, err := openLevelDBDatabaseNoCompression(rawdb.OpenOptions{
		Type:              "leveldb",
		Directory:         getRunningDatabaseDirectory(databaseEntries),
		AncientsDirectory: "",
		Namespace:         "metrics_prefix",
		Cache:             levelDBCacheSizeMB,
		Handles:           200,
		ReadOnly:          false,
		Ephemeral:         false,
	})
	if err != nil {
		return fmt.Errorf("unable to create level db database : %v", err)
	}

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    &pathDBConfig,
	})

	parentHash := common.BytesToHash(rootBytes)
	tdb, err := trie.New(trie.TrieID(parentHash), trieDb)
	if err != nil {
		return fmt.Errorf("unable to create trie database : %v", err)
	}
	var root common.Hash
	blockHeight := (databaseEntries + databaseCreationBatchSize - 1) / databaseCreationBatchSize
	writeBatch := func() error {
		var nodes *trienode.NodeSet
		root, nodes = tdb.Commit(false)
		err = trieDb.Update(root, parentHash, blockHeight, trienode.NewWithNodeSet(nodes), nil /*states*/)
		if err != nil {
			return fmt.Errorf("unable to update trie : %v", err)
		}
		tdb, err = trie.New(trie.TrieID(root), trieDb)
		if err != nil {
			return fmt.Errorf("unable to create new trie : %v", err)
		}
		parentHash = root
		blockHeight++
		return nil
	}

	batchIdx := uint64(0)
	entriesKeys := make([][]byte, 10000)
	for i := range entriesKeys {
		entriesKeys[i] = calculateIndexEncoding(uint64(i))
	}
	var updateDuration, batchDuration time.Duration
	entriesToUpdate := uint64(1)
	lastChangeEntriesCount := time.Now()
	for {
		startBatchTime := time.Now()

		// update a single entry, at random.
		startUpdateTime := time.Now()
		for i := uint64(0); i < entriesToUpdate; i++ {
			err = tdb.Update(entriesKeys[i], binary.BigEndian.AppendUint64(nil, batchIdx*10000+i))
			if err != nil {
				return fmt.Errorf("unable to update trie entry : %v", err)
			}
		}

		updateDuration = time.Since(startUpdateTime)
		stats.updateRate.Set(float64(databaseRunningUpdateSize) * float64(time.Second) / float64(updateDuration))
		stats.updates.Add(databaseRunningUpdateSize)

		batchWriteStartTime := time.Now()
		err = writeBatch()
		if err != nil {
			return fmt.Errorf("unable to write batch : %v", err)
		}
		batchDuration = time.Since(startBatchTime)
		batchWriteDuration := time.Since(batchWriteStartTime)
		stats.batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))

		if *verbose {
			fmt.Printf("update rate [%d]\tbatch rate [%d]\tbatch size [%d]\n",
				time.Second/updateDuration,
				time.Second/batchDuration,
				entriesToUpdate)
		}

		stats.batches.Inc()
		batchIdx++

		if time.Since(lastChangeEntriesCount) > 5*time.Minute {
			lastChangeEntriesCount = time.Now()
			entriesToUpdate *= 10
			if entriesToUpdate > 10000 {
				entriesToUpdate = 1
			}
		}
	}
}
