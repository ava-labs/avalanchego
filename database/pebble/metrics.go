// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	// "strconv"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/cockroachdb/pebble"
	"github.com/prometheus/client_golang/prometheus"
	// "go.uber.org/zap"
)

var levelLabels = []string{"level"}

type metrics struct {
	/*
		// total number of writes that have been delayed due to compaction
		writesDelayedCount prometheus.Counter
		// total amount of time (in ns) that writes that have been delayed due to
		// compaction
		writesDelayedDuration prometheus.Counter
		// set to 1 if there is currently at least one write that is being delayed
		// due to compaction
		writeIsDelayed prometheus.Gauge

		// number of currently alive snapshots
		aliveSnapshots prometheus.Gauge
		// number of currently alive iterators
		aliveIterators prometheus.Gauge

		// total amount of data written
		ioWrite prometheus.Counter
		// total amount of data read
		ioRead prometheus.Counter

		// total number of bytes of cached data blocks
		blockCacheSize prometheus.Gauge
		// current number of open tables
		openTables prometheus.Gauge

		// number of tables per level
		levelTableCount *prometheus.GaugeVec
		// size of each level
		levelSize *prometheus.GaugeVec
		// amount of time spent compacting each level
		levelDuration *prometheus.CounterVec
		// amount of bytes read while compacting each level
		levelReads *prometheus.CounterVec
		// amount of bytes written while compacting each level
		levelWrites *prometheus.CounterVec

		// total number memory compactions performed
		memCompactions prometheus.Counter
		// total number of level 0 compactions performed
		level0Compactions prometheus.Counter
		// total number of non-level 0 compactions performed
		nonLevel0Compactions prometheus.Counter
		// total number of seek compactions performed
		seekCompactions prometheus.Counter

		priorStats, currentStats *leveldb.DBStats
	*/

	/*

		BlockCache CacheMetrics

		Compact struct {
			// The total number of compactions, and per-compaction type counts.
			Count            int64
			DefaultCount     int64
			DeleteOnlyCount  int64
			ElisionOnlyCount int64
			MoveCount        int64
			ReadCount        int64
			RewriteCount     int64
			MultiLevelCount  int64

			// Number of compactions that are in-progress.
			NumInProgress int64
			// MarkedFiles is a count of files that are marked for
			// compaction. Such files are compacted in a rewrite compaction
			// when no other compactions are picked.
			MarkedFiles int
		}

		Flush struct {
			// The total number of flushes.
			Count           int64
			WriteThroughput ThroughputMetric
			// Number of flushes that are in-progress. In the current implementation
			// this will always be zero or one.
			NumInProgress int64
			// AsIngestCount is a monotonically increasing counter of flush operations
			// handling ingested tables.
			AsIngestCount uint64
			// AsIngestCount is a monotonically increasing counter of tables ingested as
			// flushables.
			AsIngestTableCount uint64
			// AsIngestBytes is a monotonically increasing counter of the bytes flushed
			// for flushables that originated as ingestion operations.
			AsIngestBytes uint64
		}

		Filter FilterMetrics

		Levels [numLevels]LevelMetrics

		MemTable struct {
			// The number of bytes allocated by memtables and large (flushable)
			// batches.
			Size uint64
			// The count of memtables.
			Count int64
			// The number of bytes present in zombie memtables which are no longer
			// referenced by the current DB state but are still in use by an iterator.
			ZombieSize uint64
			// The count of zombie memtables.
			ZombieCount int64
		}

		Keys struct {
			// The approximate count of internal range key set keys in the database.
			RangeKeySetsCount uint64
			// The approximate count of internal tombstones (DEL, SINGLEDEL and
			// RANGEDEL key kinds) within the database.
			TombstoneCount uint64
		}

		Snapshots struct {
			// The number of currently open snapshots.
			Count int
			// The sequence number of the earliest, currently open snapshot.
			EarliestSeqNum uint64
			// A running tally of keys written to sstables during flushes or
			// compactions that would've been elided if it weren't for open
			// snapshots.
			PinnedKeys uint64
			// A running cumulative sum of the size of keys and values written to
			// sstables during flushes or compactions that would've been elided if
			// it weren't for open snapshots.
			PinnedSize uint64
		}

		Table struct {
			// The number of bytes present in obsolete tables which are no longer
			// referenced by the current DB state or any open iterators.
			ObsoleteSize uint64
			// The count of obsolete tables.
			ObsoleteCount int64
			// The number of bytes present in zombie tables which are no longer
			// referenced by the current DB state but are still in use by an iterator.
			ZombieSize uint64
			// The count of zombie tables.
			ZombieCount int64
		}

		TableCache CacheMetrics

		// Count of the number of open sstable iterators.
		TableIters int64

		WAL struct {
			// Number of live WAL files.
			Files int64
			// Number of obsolete WAL files.
			ObsoleteFiles int64
			// Physical size of the obsolete WAL files.
			ObsoletePhysicalSize uint64
			// Size of the live data in the WAL files. Note that with WAL file
			// recycling this is less than the actual on-disk size of the WAL files.
			Size uint64
			// Physical size of the WAL files on-disk. With WAL file recycling,
			// this is greater than the live data in WAL files.
			PhysicalSize uint64
			// Number of logical bytes written to the WAL.
			BytesIn uint64
			// Number of bytes written to the WAL.
			BytesWritten uint64
		}

		LogWriter struct {
			FsyncLatency prometheus.Histogram
			record.LogWriterMetrics
		}

		private struct {
			optionsFileSize  uint64
			manifestFileSize uint64
		}
	*/

	// An estimate of the number of bytes that need to be compacted for the LSM
	// to reach a stable state.
	compactEstimatedDebt prometheus.Counter

	// Number of bytes present in sstables being written by in-progress
	// compactions. This value will be zero if there are no in-progress
	// compactions.
	compactInProgressBytes prometheus.Counter
	priorMetrics           *pebble.Metrics
}

func newMetrics(namespace string, reg prometheus.Registerer) (metrics, error) {
	m := metrics{
		compactEstimatedDebt: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "estimate_debt",
			Help:      "estimate of the number of bytes that need to be compacted for the LSM to reach a stable state",
		}),

		compactInProgressBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inprogress_bytes",
			Help:      "Number of bytes present in sstables being written by in-progress compactions",
		}),

		priorMetrics: &pebble.Metrics{},
	}

	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.compactEstimatedDebt),
		reg.Register(m.compactInProgressBytes),
	)
	return m, errs.Err
}

func (db *Database) updateMetrics() error {
	metrics := &db.metrics

	priorMetrics := metrics.priorMetrics

	// Retrieve the database stats
	currentMetrics := db.db.Metrics()
	metrics.compactEstimatedDebt.Add(float64(currentMetrics.Compact.EstimatedDebt - priorMetrics.Compact.EstimatedDebt))
	//metrics.compactInProgressBytes.Add(float64(currentMetrics.Compact.InProgressBytes - priorMetrics.Compact.InProgressBytes))
	/*
		metrics.writesDelayedDuration.Add(float64(currentStats.WriteDelayDuration - priorStats.WriteDelayDuration))
		if currentStats.WritePaused {
			metrics.writeIsDelayed.Set(1)
		} else {
			metrics.writeIsDelayed.Set(0)
		}

		metrics.aliveSnapshots.Set(float64(currentStats.AliveSnapshots))
		metrics.aliveIterators.Set(float64(currentStats.AliveIterators))

		metrics.ioWrite.Add(float64(currentStats.IOWrite - priorStats.IOWrite))
		metrics.ioRead.Add(float64(currentStats.IORead - priorStats.IORead))

		metrics.blockCacheSize.Set(float64(currentStats.BlockCacheSize))
		metrics.openTables.Set(float64(currentStats.OpenedTablesCount))

		for level, tableCounts := range currentStats.LevelTablesCounts {
			levelStr := strconv.Itoa(level)
			metrics.levelTableCount.WithLabelValues(levelStr).Set(float64(tableCounts))
			metrics.levelSize.WithLabelValues(levelStr).Set(float64(currentStats.LevelSizes[level]))

			if level < len(priorStats.LevelTablesCounts) {
				metrics.levelDuration.WithLabelValues(levelStr).Add(float64(currentStats.LevelDurations[level] - priorStats.LevelDurations[level]))
				metrics.levelReads.WithLabelValues(levelStr).Add(float64(currentStats.LevelRead[level] - priorStats.LevelRead[level]))
				metrics.levelWrites.WithLabelValues(levelStr).Add(float64(currentStats.LevelWrite[level] - priorStats.LevelWrite[level]))
			} else {
				metrics.levelDuration.WithLabelValues(levelStr).Add(float64(currentStats.LevelDurations[level]))
				metrics.levelReads.WithLabelValues(levelStr).Add(float64(currentStats.LevelRead[level]))
				metrics.levelWrites.WithLabelValues(levelStr).Add(float64(currentStats.LevelWrite[level]))
			}
		}

		metrics.memCompactions.Add(float64(currentStats.MemComp - priorStats.MemComp))
		metrics.level0Compactions.Add(float64(currentStats.Level0Comp - priorStats.Level0Comp))
		metrics.nonLevel0Compactions.Add(float64(currentStats.NonLevel0Comp - priorStats.NonLevel0Comp))
		metrics.seekCompactions.Add(float64(currentStats.SeekComp - priorStats.SeekComp))
	*/
	// update the priorStats to update the counters correctly next time this
	// method is called
	metrics.priorMetrics = currentMetrics
	return nil
}
