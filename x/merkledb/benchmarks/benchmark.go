// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
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

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	defaultDatabaseEntries    = 1000000000 // 1B
	databaseCreationBatchSize = 10000      // 10k
	databaseRunningBatchSize  = 2500       // 2.5k
	databaseRunningUpdateSize = 5000       // 5k
	defaultMetricsPort        = 3000
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

func getMerkleDBConfig(promRegistry prometheus.Registerer) merkledb.Config {
	const defaultHistoryLength = 120
	return merkledb.Config{
		BranchFactor:                merkledb.BranchFactor16,
		Hasher:                      merkledb.DefaultHasher,
		RootGenConcurrency:          0,
		HistoryLength:               defaultHistoryLength,
		ValueNodeCacheSize:          units.MiB,
		IntermediateNodeCacheSize:   1024 * units.MiB,
		IntermediateWriteBufferSize: units.KiB,
		IntermediateWriteBatchSize:  256 * units.KiB,
		Reg:                         promRegistry,
		TraceLevel:                  merkledb.NoTrace,
		Tracer:                      trace.Noop,
	}
}

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

func createGoldenDatabase() error {
	err := os.RemoveAll(getGoldenStagingDatabaseDirectory())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to remove running directory : %v\n", err)
		return err
	}
	err = os.Mkdir(getGoldenStagingDatabaseDirectory(), 0o777)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create golden staging directory : %v\n", err)
		return err
	}

	levelDB, err := leveldb.New(
		getGoldenStagingDatabaseDirectory(),
		getLevelDBConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create levelDB database : %v\n", err)
		return err
	}

	mdb, err := merkledb.New(context.Background(), levelDB, getMerkleDBConfig(nil))
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create merkleDB database : %v\n", err)
		return err
	}

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

	startInsertTime := time.Now()
	startInsertBatchTime := startInsertTime
	currentBatch := mdb.NewBatch()
	for entryIdx := uint64(0); entryIdx < *databaseEntries; entryIdx++ {
		entryHash := calculateIndexEncoding(entryIdx)
		err = currentBatch.Put(entryHash, entryHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to put value in merkleDB database : %v\n", err)
			return err
		}

		if entryIdx%databaseCreationBatchSize == (databaseCreationBatchSize - 1) {
			addDuration := time.Since(startInsertBatchTime)
			insertRate.Set(float64(databaseCreationBatchSize) * float64(time.Second) / float64(addDuration))
			insertsCounter.Add(databaseCreationBatchSize)

			batchWriteStartTime := time.Now()
			err = currentBatch.Write()
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to write value in merkleDB database : %v\n", err)
				return err
			}
			currentBatch.Reset()
			batchWriteDuration := time.Since(batchWriteStartTime)
			batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))
			batchCounter.Inc()

			startInsertBatchTime = time.Now()
		}
	}
	if (*databaseEntries)%databaseCreationBatchSize != 0 {
		err = currentBatch.Write()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write value in merkleDB database : %v\n", err)
			return err
		}
	}
	close(ticksCh)

	fmt.Print(" done!\n")
	rootID, err := mdb.GetMerkleRoot(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get root ID : %v\n", err)
		return err
	}

	fmt.Printf("Generated and inserted %d batches of size %d in %v\n",
		(*databaseEntries)/databaseCreationBatchSize, databaseCreationBatchSize, time.Since(startInsertTime))
	err = mdb.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close merkleDB database : %v\n", err)
		return err
	}
	err = levelDB.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close levelDB database : %v\n", err)
		return err
	}
	err = os.Rename(getGoldenStagingDatabaseDirectory(), getGoldenDatabaseDirectory())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to rename golden staging directory : %v\n", err)
		return err
	}
	fmt.Printf("Completed initialization with hash of %v\n", rootID.Hex())
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
	levelDB, err := leveldb.New(
		getRunningDatabaseDirectory(),
		getLevelDBConfig(),
		logging.NoLog{},
		promRegistry,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create levelDB database : %v\n", err)
		return err
	}

	mdb, err := merkledb.New(context.Background(), levelDB, getMerkleDBConfig(promRegistry))
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create merkleDB database : %v\n", err)
		return err
	}
	defer func() {
		mdb.Close()
		levelDB.Close()
	}()

	low := uint64(0)
	var deleteDuration, addDuration, updateDuration, batchDuration time.Duration
	batch := mdb.NewBatch()
	for {
		startBatchTime := time.Now()

		// delete first 2.5k keys from the beginning
		startDeleteTime := time.Now()
		for keyToDeleteIdx := low; keyToDeleteIdx < low+databaseRunningBatchSize; keyToDeleteIdx++ {
			entryHash := calculateIndexEncoding(keyToDeleteIdx)
			err = batch.Delete(entryHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to delete merkleDB entry : %v\n", err)
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
			err = batch.Put(entryHash, entryHash)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to insert merkleDB entry : %v\n", err)
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
			err = batch.Put(updateEntryKey, updateEntryValue)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to update merkleDB entry : %v\n", err)
				return err
			}
		}
		updateDuration = time.Since(startUpdateTime)
		updateRate.Set(float64(databaseRunningUpdateSize) * float64(time.Second) / float64(updateDuration))
		updatesCounter.Add(databaseRunningUpdateSize)

		batchWriteStartTime := time.Now()
		err = batch.Write()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write batch : %v\n", err)
			return err
		}
		batchDuration = time.Since(startBatchTime)
		batchWriteDuration := time.Since(batchWriteStartTime)
		batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))

		batch.Reset()

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