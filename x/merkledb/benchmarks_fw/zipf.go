// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

/*
import (
	"fmt"
	"math/rand"
	"os"
	"path"
	"time"

)
*/

func runZipfBenchmark(databaseEntries uint64, sZipf float64, vZipf float64) error {
	panic("not implemented")
	/*
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
		err = trieDb.Update(root, parentHash, blockHeight, trienode.NewWithNodeSet(nodes), nil /*states* /)
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

	batchIndex := uint64(0)
	var updateDuration, batchDuration time.Duration

	zipf := rand.NewZipf(rand.New(rand.NewSource(0)), sZipf, vZipf, databaseEntries)
	for {
		startBatchTime := time.Now()

		// update 10k entries
		startUpdateTime := time.Now()
		updateEntryValue := calculateIndexEncoding(batchIndex)

		for keyToUpdateIdx := 0; keyToUpdateIdx < databaseCreationBatchSize; keyToUpdateIdx++ {
			updateEntryKey := calculateIndexEncoding(zipf.Uint64())
			err = tdb.Update(updateEntryKey, updateEntryValue)
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
			fmt.Printf("update rate [%d] batch rate [%d]\n",
				time.Second/updateDuration,
				time.Second/batchDuration)
		}

		stats.batches.Inc()
		batchIndex++
	}
	*/
}
