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

	"github.com/ava-labs/avalanchego/utils/units"
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
	defaultDatabaseEntries       = 1_000_000_000
	databaseCreationBatchSize    = 10_000
	databaseRunningBatchSize     = databaseCreationBatchSize / 4
	databaseRunningUpdateSize    = databaseCreationBatchSize / 2
	defaultMetricsPort           = 3_000
	benchmarkRevisionHistorySize = 128
)

// TODO: Adjust these cache sizes for maximum performance
const (
	cleanCacheSizeBytes = 4 * units.GiB
	levelDBCacheSizeMB  = 6 * units.GiB / units.MiB

	// TODO: Why 200? The default is 500
	// see https://pkg.go.dev/github.com/syndtr/goleveldb@v1.0.0/leveldb/opt#Options
	openFilesCacheCapacity = 200
)

var pathDBConfig = pathdb.Config{
	StateHistory:   benchmarkRevisionHistorySize,
	CleanCacheSize: cleanCacheSizeBytes,
	DirtyCacheSize: 0,
	ReadOnly:       false,
}

var stats = struct {
	inserts        prometheus.Counter
	deletes        prometheus.Counter
	updates        prometheus.Counter
	batches        prometheus.Counter
	deleteRate     prometheus.Gauge
	updateRate     prometheus.Gauge
	insertRate     prometheus.Gauge
	batchWriteRate prometheus.Gauge
	registry       *prometheus.Registry
}{
	inserts: prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "insert_counter",
		Help:      "Total number of inserts",
	}),
	deletes: prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "deletes_counter",
		Help:      "Total number of deletes",
	}),
	updates: prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "updates_counter",
		Help:      "Total number of updates",
	}),
	batches: prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "batch",
		Help:      "Total number of batches written",
	}),
	deleteRate: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_delete_rate",
		Help:      "The rate at which elements are deleted",
	}),
	updateRate: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_update_rate",
		Help:      "The rate at which elements are updated",
	}),
	insertRate: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "entry_insert_rate",
		Help:      "The rate at which elements are inserted",
	}),
	batchWriteRate: prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "merkledb_bench",
		Name:      "batch_write_rate",
		Help:      "The rate at which the batch was written",
	}),

	registry: prometheus.NewRegistry(),
}

// command line arguments
var (
	databaseEntries = pflag.Uint64("n", defaultDatabaseEntries, "number of database entries")
	httpMetricPort  = pflag.Uint64("p", defaultMetricsPort, "default metrics port")
	verbose         = pflag.Bool("v", false, "verbose")
)

func Must[T any](obj T, err error) T {
	if err != nil {
		panic(err)
	}
	return obj
}

func getGoldenStagingDatabaseDirectory(databaseEntries uint64) string {
	return path.Join(Must(os.Getwd()), fmt.Sprintf("db-bench-test-golden-staging-%d", databaseEntries))
}

func getGoldenDatabaseDirectory(databaseEntries uint64) string {
	return path.Join(Must(os.Getwd()), fmt.Sprintf("db-bench-test-golden-%d", databaseEntries))
}

func getRunningDatabaseDirectory(databaseEntries uint64) string {
	return path.Join(Must(os.Getwd()), fmt.Sprintf("db-bench-test-running-%d", databaseEntries))
}

func calculateIndexEncoding(idx uint64) []byte {
	var entryEncoding [8]byte
	binary.NativeEndian.PutUint64(entryEncoding[:], idx)
	entryHash := sha256.Sum256(entryEncoding[:])
	return entryHash[:]
}

func createGoldenDatabase(databaseEntries uint64) error {
	stagingDirectory := getGoldenStagingDatabaseDirectory(databaseEntries)
	err := os.RemoveAll(stagingDirectory)
	if err != nil {
		return fmt.Errorf("unable to remove running directory : %v", err)
	}
	err = os.Mkdir(stagingDirectory, 0o777)
	if err != nil {
		return fmt.Errorf("unable to create golden staging directory : %v", err)
	}

	ldb, err := rawdb.NewLevelDBDatabase(stagingDirectory, levelDBCacheSizeMB, openFilesCacheCapacity, "metrics_prefix", false)
	if err != nil {
		return fmt.Errorf("unable to create level db database : %v", err)
	}

	trieDb := triedb.NewDatabase(ldb, &triedb.Config{
		Preimages: false,
		IsVerkle:  false,
		HashDB:    nil,
		PathDB:    &pathDBConfig,
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
	blockHeight := uint64(1)
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

	startInsertTime := time.Now()
	startInsertBatchTime := startInsertTime
	for entryIdx := range databaseEntries {
		entryHash := calculateIndexEncoding(entryIdx)
		tdb.Update(entryHash, entryHash)

		if entryIdx%databaseCreationBatchSize == (databaseCreationBatchSize - 1) {
			addDuration := time.Since(startInsertBatchTime)
			stats.insertRate.Set(float64(databaseCreationBatchSize) * float64(time.Second) / float64(addDuration))
			stats.inserts.Add(databaseCreationBatchSize)

			batchWriteStartTime := time.Now()

			err = writeBatch()
			if err != nil {
				return fmt.Errorf("unable to write value in database : %v", err)
			}

			batchWriteDuration := time.Since(batchWriteStartTime)
			stats.batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))
			stats.batches.Inc()

			startInsertBatchTime = time.Now()
		}
	}
	// write the last batch. In our default case, there won't be a last batch
	if databaseEntries%databaseCreationBatchSize != 0 {
		err = writeBatch()
		if err != nil {
			return fmt.Errorf("unable to write value in database : %v", err)
		}
	}
	err = trieDb.Commit(root, false)
	if err != nil {
		return fmt.Errorf("unable to commit trie : %v", err)
	}
	close(ticksCh)
	fmt.Print(" done!\n")

	if err = trieDb.Close(); err != nil {
		return fmt.Errorf("unable to close trie database : %v", err)
	}

	if err = ldb.Close(); err != nil {
		return fmt.Errorf("unable to close levelDB database : %v", err)
	}

	fmt.Printf("Generated and inserted %d batches of size %d in %v\n",
		databaseEntries/databaseCreationBatchSize, databaseCreationBatchSize, time.Since(startInsertTime))

	if err = os.WriteFile(path.Join(stagingDirectory, "root.txt"), root.Bytes(), 0o644); err != nil {
		return fmt.Errorf("unable to save root : %v", err)
	}

	err = os.Rename(getGoldenStagingDatabaseDirectory(databaseEntries), getGoldenDatabaseDirectory(databaseEntries))
	if err != nil {
		return fmt.Errorf("unable to rename golden staging directory : %v", err)
	}
	fmt.Printf("Completed initialization with hash of %v\n", root.Hex())
	return nil
}

func resetRunningDatabaseDirectory(databaseEntries uint64) error {
	runningDir := getRunningDatabaseDirectory(databaseEntries)
	if _, err := os.Stat(runningDir); err == nil {
		err := os.RemoveAll(runningDir)
		if err != nil {
			return fmt.Errorf("unable to remove running directory : %v", err)
		}
	}
	err := os.Mkdir(runningDir, 0o777)
	if err != nil {
		return fmt.Errorf("unable to create running directory : %v", err)
	}
	err = CopyDirectory(getGoldenDatabaseDirectory(databaseEntries), runningDir)
	if err != nil {
		return fmt.Errorf("unable to duplicate golden directory : %v", err)
	}
	return nil
}

func runBenchmark(databaseEntries uint64) error {
	rootBytes, err := os.ReadFile(path.Join(getRunningDatabaseDirectory(databaseEntries), "root.txt"))
	if err != nil {
		return fmt.Errorf("unable to read root : %v", err)
	}

	ldb, err := rawdb.Open(rawdb.OpenOptions{
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
		err = trieDb.Commit(root, false)
		if err != nil {
			return fmt.Errorf("unable to commit trie : %v", err)
		}
		tdb, err = trie.New(trie.TrieID(root), trieDb)
		if err != nil {
			return fmt.Errorf("unable to create new trie : %v", err)
		}
		parentHash = root
		blockHeight++
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
				return fmt.Errorf("unable to delete trie entry : %v", err)
			}
		}
		deleteDuration = time.Since(startDeleteTime)
		stats.deleteRate.Set(float64(databaseRunningBatchSize) * float64(time.Second) / float64(deleteDuration))
		stats.deletes.Add(databaseRunningBatchSize)

		// add 2.5k past end.
		startInsertTime := time.Now()
		for keyToAddIdx := low + databaseEntries; keyToAddIdx < low+databaseEntries+databaseRunningBatchSize; keyToAddIdx++ {
			entryHash := calculateIndexEncoding(keyToAddIdx)
			err = tdb.Update(entryHash, entryHash)
			if err != nil {
				return fmt.Errorf("unable to insert trie entry : %v", err)
			}
		}
		addDuration = time.Since(startInsertTime)
		stats.insertRate.Set(float64(databaseRunningBatchSize) * float64(time.Second) / float64(addDuration))
		stats.inserts.Add(databaseRunningBatchSize)

		// update middle 5k entries
		startUpdateTime := time.Now()
		updateEntryValue := calculateIndexEncoding(low)
		for keyToUpdateIdx := low + (databaseEntries / 2); keyToUpdateIdx < low+(databaseEntries/2)+databaseRunningUpdateSize; keyToUpdateIdx++ {
			updateEntryKey := calculateIndexEncoding(keyToUpdateIdx)
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
			fmt.Printf("delete rate [%d]	update rate [%d]	insert rate [%d]	batch rate [%d]\n",
				time.Second/deleteDuration,
				time.Second/updateDuration,
				time.Second/addDuration,
				time.Second/batchDuration)
		}

		stats.batches.Inc()
		low += databaseRunningBatchSize
	}
}

func setupMetrics() error {
	if err := prometheus.Register(stats.registry); err != nil {
		return err
	}
	stats.registry.MustRegister(stats.inserts)
	stats.registry.MustRegister(stats.deletes)
	stats.registry.MustRegister(stats.updates)
	stats.registry.MustRegister(stats.batches)
	stats.registry.MustRegister(stats.deleteRate)
	stats.registry.MustRegister(stats.updateRate)
	stats.registry.MustRegister(stats.insertRate)
	stats.registry.MustRegister(stats.batchWriteRate)

	http.Handle("/metrics", promhttp.Handler())

	prometheusServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", *httpMetricPort),
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		err := prometheusServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			panic(fmt.Sprintf("unable to listen and serve : %v\n", err))
		}
	}()
	return nil
}

func main() {
	pflag.Parse()

	if err := setupMetrics(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to setup metrics : %v\n", err)
		os.Exit(1)
	}

	goldenDir := getGoldenDatabaseDirectory(*databaseEntries)
	if _, err := os.Stat(goldenDir); os.IsNotExist(err) {
		// create golden image.
		if err := createGoldenDatabase(*databaseEntries); err != nil {
			fmt.Fprintf(os.Stderr, "unable to create golden database : %v\n", err)
			os.Exit(1)
		}
	}
	if err := resetRunningDatabaseDirectory(*databaseEntries); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to reset running database directory: %v\n", err)
		os.Exit(1)
	}
	if err := runBenchmark(*databaseEntries); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to run benchmark: %v\n", err)
		os.Exit(1)
	}
}
