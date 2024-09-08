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

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	defaultDatabaseEntries    = 1000000
	databaseCreationBatchSize = 10000
	defaultHistoryLength      = 300
	databaseRunningBatchSize  = 2500
	databaseRunningUpdateSize = 5000
)

var databaseEntries = pflag.Uint64("n", defaultDatabaseEntries, "number of database entries")

func getMerkleDBConfig(promRegistry prometheus.Registerer) merkledb.Config {
	return merkledb.Config{
		BranchFactor:                merkledb.BranchFactor16,
		Hasher:                      merkledb.DefaultHasher,
		RootGenConcurrency:          0,
		HistoryLength:               defaultHistoryLength,
		ValueNodeCacheSize:          units.MiB,
		IntermediateNodeCacheSize:   units.MiB,
		IntermediateWriteBufferSize: units.KiB,
		IntermediateWriteBatchSize:  256 * units.KiB,
		Reg:                         promRegistry,
		TraceLevel:                  merkledb.InfoTrace,
		Tracer:                      trace.Noop,
	}
}

func getGoldenDatabaseDirectory() string {
	wd, _ := os.Getwd()
	return path.Join(wd, fmt.Sprintf("db-bench-test-golden-%d", databaseEntries))
}

func getRunningDatabaseDirectory() string {
	wd, _ := os.Getwd()
	return path.Join(wd, fmt.Sprintf("db-bench-test-running-%d", databaseEntries))
}

func calculateIndexEncoding(idx uint64) []byte {
	var entryEncoding [8]byte
	binary.NativeEndian.PutUint64(entryEncoding[:], idx)
	entryHash := sha256.Sum256(entryEncoding[:])
	return entryHash[:]
}

func createGoldenDatabase() error {
	promRegistry := prometheus.NewRegistry()

	levelDB, err := leveldb.New(
		getGoldenDatabaseDirectory(),
		nil,
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

	startInsertTime := time.Now()
	var currentBatch database.Batch
	for entryIdx := uint64(0); entryIdx < *databaseEntries; entryIdx++ {
		if entryIdx%databaseCreationBatchSize == 0 {
			currentBatch = mdb.NewBatch()
		}

		entryHash := calculateIndexEncoding(entryIdx)
		err = currentBatch.Put(entryHash, entryHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to put value in merkleDB database : %v\n", err)
			return err
		}

		if entryIdx%databaseCreationBatchSize == (databaseCreationBatchSize - 1) {
			err = currentBatch.Write()
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to write value in merkleDB database : %v\n", err)
				return err
			}
		}
	}
	if (*databaseEntries)%databaseCreationBatchSize != 0 {
		err = currentBatch.Write()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write value in merkleDB database : %v\n", err)
			return err
		}
	}
	rootID, err := mdb.GetMerkleRoot(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get root ID : %v\n", err)
		return err
	}

	fmt.Printf("Generated and inserted %d batches of size %d in %v\n",
		(*databaseEntries)/databaseCreationBatchSize, databaseCreationBatchSize, time.Since(startInsertTime))
	err = mdb.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close levelDB database : %v\n", err)
		return err
	}
	err = levelDB.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to close merkleDB database : %v\n", err)
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
	promRegistry := prometheus.NewRegistry()
	http.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              ":8080",
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "unable to listen and serve : %v\n", err)
		}
	}()

	writesCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "writes",
		Help:      "Total number of keys written",
	})
	batchCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "merkledb_bench",
		Name:      "batch",
		Help:      "Total number of batches written",
	})
	promRegistry.MustRegister(writesCounter)
	promRegistry.MustRegister(batchCounter)

	levelDB, err := leveldb.New(
		getRunningDatabaseDirectory(),
		nil,
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
	for {
		startBatchTime := time.Now()
		batch := mdb.NewBatch()

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

		// update middle 5k entries
		startUpdateTime := time.Now()
		for keyToUpdateIdx := low + ((*databaseEntries) / 2); keyToUpdateIdx < low+((*databaseEntries)/2)+databaseRunningUpdateSize; keyToUpdateIdx++ {
			updateEntryKey := calculateIndexEncoding(keyToUpdateIdx)
			updateEntryValue := calculateIndexEncoding(keyToUpdateIdx - ((*databaseEntries) / 2))
			err = batch.Put(updateEntryKey, updateEntryValue)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to update merkleDB entry : %v\n", err)
				return err
			}
		}
		updateDuration = time.Since(startUpdateTime)

		err = batch.Write()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to write batch : %v\n", err)
			return err
		}
		batchDuration = time.Since(startBatchTime)

		fmt.Printf("delete rate [%d]	update rate [%d]	insert rate [%d]	batch rate [%d]\n",
			time.Second/deleteDuration,
			time.Second/updateDuration,
			time.Second/addDuration,
			time.Second/batchDuration)
		writesCounter.Add(databaseRunningBatchSize + databaseRunningBatchSize + databaseRunningUpdateSize)
		batchCounter.Inc()
	}

	// return nil
}

func main() {
	pflag.Parse()

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
