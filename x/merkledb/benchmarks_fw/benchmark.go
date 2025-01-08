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
	"github.com/spf13/cobra"
	
	"avalabs.org/ffi/v2"
)

const (
	defaultDatabaseEntries       = 0
	databaseCreationBatchSize    = 10_000
	databaseRunningBatchSize     = databaseCreationBatchSize / 4
	databaseRunningUpdateSize    = databaseCreationBatchSize / 2
	defaultMetricsPort           = 3_000
	benchmarkRevisionHistorySize = 128
	defaultZipfS                 = float64(1.2)
	defaultZipfV                 = float64(1.0)
)

// TODO: Adjust these cache sizes for maximum performance
const (
	cleanCacheSizeBytes = 1 * units.GiB
	levelDBCacheSizeMB  = 3 * units.GiB / (2 * units.MiB)
)

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

// command line arguments parameters
var (
	databaseEntries *uint64
	httpMetricPort  *uint64
	verbose         *bool
	sZipf           *float64
	vZipf           *float64
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

	fwdb := new(firewood.Firewood)

	fwdb.Get([]byte("foo"))

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

	blockHeight := uint64(1)

	startInsertTime := time.Now()
	startInsertBatchTime := startInsertTime

	rows := make([]firewood.KeyValue, databaseCreationBatchSize)
	for entryIdx := range databaseEntries {
		entryHash := calculateIndexEncoding(entryIdx)
		rows[entryIdx%databaseCreationBatchSize] = firewood.KeyValue{
			Key:   entryHash,
			Value: entryHash,
		}

		if entryIdx%databaseCreationBatchSize == (databaseCreationBatchSize - 1) {
			addDuration := time.Since(startInsertBatchTime)
			stats.insertRate.Set(float64(databaseCreationBatchSize) * float64(time.Second) / float64(addDuration))
			stats.inserts.Add(databaseCreationBatchSize)

			batchWriteStartTime := time.Now()

			fwdb.Batch(rows)
			rows = make([]firewood.KeyValue, databaseCreationBatchSize)
			blockHeight++

			batchWriteDuration := time.Since(batchWriteStartTime)
			stats.batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))
			stats.batches.Inc()

			startInsertBatchTime = time.Now()
		}
	}
	close(ticksCh)
	fmt.Print(" done!\n")

	fmt.Printf("Generated and inserted %d batches of size %d in %v\n",
		databaseEntries/databaseCreationBatchSize, databaseCreationBatchSize, time.Since(startInsertTime))

	err = os.Rename(getGoldenStagingDatabaseDirectory(databaseEntries), getGoldenDatabaseDirectory(databaseEntries))
	if err != nil {
		return fmt.Errorf("unable to rename golden staging directory : %v", err)
	}
	// fmt.Printf("Completed initialization with hash of %v\n", root.Hex())
	fmt.Printf("Completed initialization with hash of TODO\n")
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
	rootCmd := &cobra.Command{
		Use:   "benchmarks_eth",
		Short: "benchmarks_eth",
		RunE: func(*cobra.Command, []string) error {
			fmt.Printf("Please specify which type of benchmark you'd like to run : [tenkrandom, zipf, single]\n")
			return nil
		},
	}
	databaseEntries = rootCmd.PersistentFlags().Uint64("n", defaultDatabaseEntries, "Number of database entries in golden database")
	httpMetricPort = rootCmd.PersistentFlags().Uint64("p", defaultMetricsPort, "HTTP metrics port")
	verbose = rootCmd.PersistentFlags().Bool("verbose", false, "Verbose mode")
	rootCmd.MarkPersistentFlagRequired("n")

	tenkrandomCmd := &cobra.Command{
		Use:   "tenkrandom",
		Short: "Run the tenkrandom benchmark",
		RunE: func(*cobra.Command, []string) error {
			tenkrandomBenchmark()
			return nil
		},
	}
	zipfCmd := &cobra.Command{
		Use:   "zipf",
		Short: "Run the zipf benchmark",
		PreRun: func(cmd *cobra.Command, args []string) {
			if *sZipf <= 1.0 {
				fmt.Fprintf(os.Stderr, "The s value for Zipf distribution must be greater than 1\n")
				os.Exit(1)
			}
			if *vZipf < 1.0 {
				fmt.Fprintf(os.Stderr, "The v value for Zipf distribution must be greater or equal to 1\n")
				os.Exit(1)
			}
		},
		RunE: func(*cobra.Command, []string) error {
			zipfBenchmark()
			return nil
		},
	}
	sZipf = zipfCmd.PersistentFlags().Float64("s", defaultZipfS, "s (Zipf distribution = [(v+k)^(-s)], Default = 1.00)")
	vZipf = zipfCmd.PersistentFlags().Float64("v", defaultZipfV, "v (Zipf distribution = [(v+k)^(-s)], Default = 2.7)")
	singleCmd := &cobra.Command{
		Use:   "single",
		Short: "Run the single benchmark",
		RunE: func(*cobra.Command, []string) error {
			singleBenchmark()
			return nil
		},
	}

	rootCmd.AddCommand(tenkrandomCmd, zipfCmd, singleCmd)

	if err := setupMetrics(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to setup metrics : %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "tmpnetctl failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func commonBenchmarkBootstrap() {
	if *databaseEntries == 0 {
		fmt.Fprintf(os.Stderr, "Please specify number of database entries by using the --n flag\n")
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
}

func tenkrandomBenchmark() {
	commonBenchmarkBootstrap()

	if err := runTenkrandomBenchmark(*databaseEntries); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to run benchmark: %v\n", err)
		os.Exit(1)
	}
}

func zipfBenchmark() {
	commonBenchmarkBootstrap()

	if err := runZipfBenchmark(*databaseEntries, *sZipf, *vZipf); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to run benchmark: %v\n", err)
		os.Exit(1)
	}
}

func singleBenchmark() {
	commonBenchmarkBootstrap()

	if err := runSingleBenchmark(*databaseEntries); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to run benchmark: %v\n", err)
		os.Exit(1)
	}
}
