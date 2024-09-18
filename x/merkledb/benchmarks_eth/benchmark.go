// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

const (
	defaultDatabaseEntries    = 1000000000 // 1B
	databaseCreationBatchSize = 10000      // 10k
	databaseRunningBatchSize  = 2500       // 2.5k
	databaseRunningUpdateSize = 5000       // 5k
	defaultMetricsPort        = 3000
	levelDBCacheSize          = 6_000
	firewoodNumberOfRevisions = 120
)

var (
	databaseEntries = pflag.Uint64("n", defaultDatabaseEntries, "number of database entries")
	httpMetricPort  = pflag.Uint64("p", defaultMetricsPort, "default metrics port")
	verbose         = pflag.Bool("v", false, "verbose")

	insertsCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "insert_counter",
		Help:      "Total number of inserts",
	})
	deletesCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "deletes_counter",
		Help:      "Total number of deletes",
	})
	updatesCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "updates_counter",
		Help:      "Total number of updates",
	})
	batchCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "batch",
		Help:      "Total number of batches written",
	})
	deleteRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_delete_rate",
		Help:      "The rate at which elements are deleted",
	})
	updateRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_update_rate",
		Help:      "The rate at which elements are updated",
	})
	insertRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_insert_rate",
		Help:      "The rate at which elements are inserted",
	})
	batchWriteRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "batch_write_rate",
		Help:      "The rate at which the batch was written",
	})
	promRegistry = prometheus.NewRegistry()
)

func getGoldenStagingDatabaseDirectory() string {
	wd, _ := os.Getwd()
	return path.Join(wd, fmt.Sprintf("db-bench-test-golden-staging-%d", *databaseEntries))
}

func getGoldenDatabaseDirectory() string {
	wd, _ := os.Getwd()
	return path.Join(wd, fmt.Sprintf("db-bench-test-golden-%d", *databaseEntries))
}

func getRunningDatabaseDirectory() string {
	wd, _ := os.Getwd()
	return path.Join(wd, fmt.Sprintf("db-bench-test-running-%d", *databaseEntries))
}

func calculateIndexEncoding(idx uint64) []byte {
	var entryEncoding [8]byte
	binary.NativeEndian.PutUint64(entryEncoding[:], idx)
	entryHash := sha256.Sum256(entryEncoding[:])
	return entryHash[:]
}

func getPathDBConfig() *pathdb.Config {
	return &pathdb.Config{
		StateHistory:   firewoodNumberOfRevisions, // keep the same requiremenet across each db
		CleanCacheSize: 6 * 1024 * 1024 * 1024,    // 4GB - this is a guess, but I assume we're currently underutilizing memory by a lot
		DirtyCacheSize: 6 * 1024 * 1024 * 1024,    // 4GB - this is a guess, but I assume we're currently underutilizing memory by a lot
	}
}

func createGoldenDatabase() error {
	stagingDirectory := getGoldenStagingDatabaseDirectory()
	err := os.RemoveAll(stagingDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to remove running directory : %v\n", err)
		return err
	}
	err = os.Mkdir(stagingDirectory, 0o777)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create golden staging directory : %v\n", err)
		return err
	}

	ldb, err := rawdb.NewLevelDBDatabase(stagingDirectory, levelDBCacheSize, 200, "metrics_prefix", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create level db database : %v\n", err)
		return err
	}

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    getPathDBConfig(),
	})
	tdb := trie.NewEmpty(trieDb)

	fmt.Print("Initializing database.")
	ticksCh := make(chan interface{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ticksCh:
				return
			case <-t.C:
				fmt.Print(".")
			}
		}
	}()

	var root common.Hash
	parentHash := types.EmptyRootHash
	writeBatch := func() error {
		var nodes *trienode.NodeSet
		root, nodes = tdb.Commit(false)
		err = trieDb.Update(root, parentHash, 0 /*block*/, trienode.NewWithNodeSet(nodes), nil /*states*/)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to update trie : %v\n", err)
			return err
		}
		err = trieDb.Commit(root, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to commit trie : %v\n", err)
			return err
		}
		tdb, err = trie.New(trie.TrieID(root), trieDb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create new trie : %v\n", err)
			return err
		}
		parentHash = root
		return nil
	}

	startInsertTime := time.Now()
	startInsertBatchTime := startInsertTime
	for entryIdx := uint64(0); entryIdx < *databaseEntries; entryIdx++ {
		entryHash := calculateIndexEncoding(entryIdx)
		tdb.Update(entryHash, entryHash)

		if entryIdx%databaseCreationBatchSize == (databaseCreationBatchSize - 1) {
			addDuration := time.Since(startInsertBatchTime)
			insertRate.Set(float64(databaseCreationBatchSize) * float64(time.Second) / float64(addDuration))
			insertsCounter.Add(databaseCreationBatchSize)

			batchWriteStartTime := time.Now()

			err = writeBatch()
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to write value in database : %v\n", err)
				return err
			}

			batchWriteDuration := time.Since(batchWriteStartTime)
			batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))
			batchCounter.Inc()

			startInsertBatchTime = time.Now()
		}
	}
	if (*databaseEntries)%databaseCreationBatchSize != 0 {
		err = writeBatch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write value in database : %v\n", err)
			return err
		}
	}
	close(ticksCh)
	fmt.Print(" done!\n")

	fmt.Printf("Generated and inserted %d batches of size %d in %v\n",
		(*databaseEntries)/databaseCreationBatchSize, databaseCreationBatchSize, time.Since(startInsertTime))
	err = trieDb.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close trie database : %v\n", err)
		return err
	}
	err = ldb.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close levelDB database : %v\n", err)
		return err
	}

	err = os.WriteFile(path.Join(stagingDirectory, "root.txt"), root.Bytes(), 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to save root : %v\n", err)
		return err
	}

	err = os.Rename(getGoldenStagingDatabaseDirectory(), getGoldenDatabaseDirectory())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to rename golden staging directory : %v\n", err)
		return err
	}
	fmt.Printf("Completed initialization with hash of %v\n", root.Hex())
	return nil
}

func resetRunningDatabaseDirectory() error {
	runningDir := getRunningDatabaseDirectory()
	if _, err := os.Stat(runningDir); err == nil {
		err := os.RemoveAll(runningDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to remove running directory : %v\n", err)
			return err
		}
	}
	err := os.Mkdir(runningDir, 0o777)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create running directory : %v\n", err)
		return err
	}
	err = CopyDirectory(getGoldenDatabaseDirectory(), runningDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to duplicate golden directory : %v\n", err)
		return err
	}
	return nil
}

func runBenchmark() error {
	rootBytes, err := os.ReadFile(path.Join(getRunningDatabaseDirectory(), "root.txt"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to read root : %v\n", err)
		return err
	}

	ldb, err := rawdb.Open(rawdb.OpenOptions{
		Type:              "leveldb",
		Directory:         getRunningDatabaseDirectory(),
		AncientsDirectory: "",
		Namespace:         "metrics_prefix",
		Cache:             levelDBCacheSize,
		Handles:           200,
		ReadOnly:          false,
		Ephemeral:         false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create level db database : %v\n", err)
		return err
	}

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    getPathDBConfig(),
	})

	parentHash := common.BytesToHash(rootBytes)
	tdb, err := trie.New(trie.TrieID(parentHash), trieDb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create trie database : %v\n", err)
		return err
	}
	var root common.Hash
	writeBatch := func() error {
		var nodes *trienode.NodeSet
		root, nodes = tdb.Commit(false)
		err = trieDb.Update(root, parentHash, 0 /*block*/, trienode.NewWithNodeSet(nodes), nil /*states*/)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to update trie : %v\n", err)
			return err
		}
		err = trieDb.Commit(root, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to commit trie : %v\n", err)
			return err
		}
		tdb, err = trie.New(trie.TrieID(root), trieDb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create new trie : %v\n", err)
			return err
		}
		parentHash = root
		return nil
	}

	low := uint64(0)
	var deleteDuration, addDuration, updateDuration, batchDuration time.Duration
	for {
		startBatchTime := time.Now()

		// delete first 2.5k keys from the beginning
		startDeleteTime := time.Now()
		for keyToDeleteIdx := low; keyToDeleteIdx < low+databaseRunningBatchSize; keyToDeleteIdx++ {
			entryHash := calculateIndexEncoding(keyToDeleteIdx)
			err = tdb.Delete(entryHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to delete trie entry : %v\n", err)
				return err
			}
		}
		deleteDuration = time.Since(startDeleteTime)
		deleteRate.Set(float64(databaseRunningBatchSize) * float64(time.Second) / float64(deleteDuration))
		deletesCounter.Add(databaseRunningBatchSize)

		// add 2.5k past end.
		startInsertTime := time.Now()
		for keyToAddIdx := low + (*databaseEntries); keyToAddIdx < low+(*databaseEntries)+databaseRunningBatchSize; keyToAddIdx++ {
			entryHash := calculateIndexEncoding(keyToAddIdx)
			err = tdb.Update(entryHash, entryHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to insert trie entry : %v\n", err)
				return err
			}
		}
		addDuration = time.Since(startInsertTime)
		insertRate.Set(float64(databaseRunningBatchSize) * float64(time.Second) / float64(addDuration))
		insertsCounter.Add(databaseRunningBatchSize)

		// update middle 5k entries
		startUpdateTime := time.Now()
		updateEntryValue := calculateIndexEncoding(low)
		for keyToUpdateIdx := low + ((*databaseEntries) / 2); keyToUpdateIdx < low+((*databaseEntries)/2)+databaseRunningUpdateSize; keyToUpdateIdx++ {
			updateEntryKey := calculateIndexEncoding(keyToUpdateIdx)
			err = tdb.Update(updateEntryKey, updateEntryValue)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to update trie entry : %v\n", err)
				return err
			}
		}
		updateDuration = time.Since(startUpdateTime)
		updateRate.Set(float64(databaseRunningUpdateSize) * float64(time.Second) / float64(updateDuration))
		updatesCounter.Add(databaseRunningUpdateSize)

		batchWriteStartTime := time.Now()
		err = writeBatch()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write batch : %v\n", err)
			return err
		}
		batchDuration = time.Since(startBatchTime)
		batchWriteDuration := time.Since(batchWriteStartTime)
		batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))

		if *verbose {
			fmt.Printf("delete rate [%d]	update rate [%d]	insert rate [%d]	batch rate [%d]\n",
				time.Second/deleteDuration,
				time.Second/updateDuration,
				time.Second/addDuration,
				time.Second/batchDuration)
		}

		batchCounter.Inc()
		low += databaseRunningBatchSize
	}

	// return nil
}

func setupMetrics() error {
	if err := prometheus.Register(promRegistry); err != nil {
		return err
	}
	promRegistry.MustRegister(insertsCounter)
	promRegistry.MustRegister(deletesCounter)
	promRegistry.MustRegister(updatesCounter)
	promRegistry.MustRegister(batchCounter)
	promRegistry.MustRegister(deleteRate)
	promRegistry.MustRegister(updateRate)
	promRegistry.MustRegister(insertRate)
	promRegistry.MustRegister(batchWriteRate)

	http.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", *httpMetricPort),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "unable to listen and serve : %v\n", err)
		}
	}()
	return nil
}

func main() {
	pflag.Parse()

	if setupMetrics() != nil {
		os.Exit(1)
		return
	}

	goldenDir := getGoldenDatabaseDirectory()
	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		// create golden image.
		if createGoldenDatabase() != nil {
			os.Exit(1)
			return
		}
	}
	if resetRunningDatabaseDirectory() != nil {
		os.Exit(1)
		return
	}
	if runBenchmark() != nil {
		os.Exit(1)
		return
	}
}
