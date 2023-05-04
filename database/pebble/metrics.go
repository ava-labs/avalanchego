// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebble

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/cockroachdb/pebble"
	"github.com/prometheus/client_golang/prometheus"
)

// var levelLabels = []string{"level"}

type metrics struct {
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

		}

		Flush struct {

			WriteThroughput ThroughputMetric
		}

		Filter FilterMetrics

		Levels [numLevels]LevelMetrics

		TableCache CacheMetrics

		// Count of the number of open sstable iterators.
		TableIters int64

		LogWriter struct {
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

	// Number of compactions that are in-progress
	compactNumInProgress prometheus.Counter

	// MarkedFiles is a count of files that are marked for
	// compaction. Such files are compacted in a rewrite compaction
	// when no other compactions are picked.
	compactMarkedFiles prometheus.Counter

	// The total number of flushes.
	flushCount prometheus.Counter

	// Number of flushes that are in-progress. In the current implementation
	// this will always be zero or one.
	flushNumInProgress prometheus.Gauge

	// AsIngestCount is a monotonically increasing counter of flush operations
	// handling ingested tables.
	flushAsIngestCount prometheus.Counter

	// AsIngestTableCount is a monotonically increasing counter of tables ingested as
	// flushables.
	flushAsIngestTableCount prometheus.Counter

	// AsIngestBytes is a monotonically increasing counter of the bytes flushed
	// for flushables that originated as ingestion operations.
	flushAsIngestBytes prometheus.Counter

	// The number of bytes allocated by memtables and large (flushable) batches.
	memtableSize prometheus.Counter

	// The count of memtables.
	memtableCount prometheus.Counter

	// The number of bytes present in zombie memtables which are no longer
	// referenced by the current DB state but are still in use by an iterator.
	memtableZombieSize prometheus.Counter

	// The count of zombie memtables.
	memtableZombieCount prometheus.Gauge

	// The approximate count of internal range key set keys in the database.
	keysRangeKeySetsCount prometheus.Counter

	// The approximate count of internal tombstones (DEL, SINGLEDEL and
	// RANGEDEL key kinds) within the database.
	keysTombstoneCount prometheus.Counter

	// The number of currently open snapshots.
	snapshotsCount prometheus.Counter

	// The sequence number of the earliest, currently open snapshot.
	snapshotsEarliestSeqNum prometheus.Gauge

	// A running tally of keys written to sstables during flushes or
	// compactions that would've been elided if it weren't for open
	// snapshots.
	snapshotsPinnedKeys prometheus.Gauge

	// A running cumulative sum of the size of keys and values written to
	// sstables during flushes or compactions that would've been elided if
	// it weren't for open snapshots.
	snapshotsPinnedSize prometheus.Gauge

	// The number of bytes present in obsolete tables which are no longer
	// referenced by the current DB state or any open iterators.
	tableObsoleteSize prometheus.Gauge

	// The count of obsolete tables.
	tableObsoleteCount prometheus.Gauge

	// The number of bytes present in zombie tables which are no longer
	// referenced by the current DB state but are still in use by an iterator.
	tableZombieSize prometheus.Gauge

	// The count of zombie tables.
	tableZombieCount prometheus.Gauge

	// Number of live WAL files.
	walFiles prometheus.Gauge

	// Number of obsolete WAL files.
	walObsoleteFiles prometheus.Gauge

	// Physical size of the obsolete WAL files.
	walObsoletePhysicalSize prometheus.Gauge

	// Size of the live data in the WAL files. Note that with WAL file
	// recycling this is less than the actual on-disk size of the WAL files.
	walSize prometheus.Gauge

	// Physical size of the WAL files on-disk. With WAL file recycling,
	// this is greater than the live data in WAL files.
	walPhysicalSize prometheus.Gauge

	// Number of logical bytes written to the WAL.
	walBytesIn prometheus.Counter

	// Number of bytes written to the WAL.
	walBytesWritten prometheus.Counter

	logwriterFsyncLatency prometheus.Histogram

	priorMetrics *pebble.Metrics
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

		compactNumInProgress: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "num_inprogress",
			Help:      "Number of compactions that are in-progress",
		}),

		compactMarkedFiles: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "marked_files",
			Help:      "count of files that are marked for compaction",
		}),

		flushCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "flush_count",
			Help:      "The total number of flushes",
		}),

		flushNumInProgress: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "flush_numinprogress",
			Help:      "Number of flushes that are in-progress",
		}),

		flushAsIngestCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flush_ingest_count",
			Help:      "counter of flush operations handling ingested tables.",
		}),

		flushAsIngestTableCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flush_ingestitable_count",
			Help:      "counter of tables ingested as flushables",
		}),

		flushAsIngestBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flush_asingest_bytes",
			Help:      "counter of the bytes flushed for flushables that originated as ingestion operations.",
		}),

		memtableSize: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "memtable_size",
			Help:      "The number of bytes allocated by memtables and large (flushable) batches",
		}),

		memtableCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "memtable_count",
			Help:      "The count of memtables",
		}),

		memtableZombieSize: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "memtable_zombiesize",
			Help:      "The number of bytes present in zombie memtables",
		}),

		memtableZombieCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "memtable_zombiesize",
			Help:      "The count of zombie memtables",
		}),

		keysRangeKeySetsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_rangekey_count",
			Help:      "The approximate count of internal range key set keys in the database",
		}),

		keysTombstoneCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_tombstone_count",
			Help:      "The approximate count of internal tombstones (DEL, SINGLEDEL and RANGEDEL key kinds) within the database.",
		}),

		snapshotsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "snapshots_count",
			Help:      "The number of currently open snapshots.",
		}),

		snapshotsEarliestSeqNum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "snapshots_earliest_seqnum",
			Help:      "The sequence number of the earliest, currently open snapshot.",
		}),

		snapshotsPinnedKeys: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "snapshots_pinned_keys",
			Help:      "A running tally of keys written to sstables during flushes or compactions that would've been elided if it weren't for open snapshots.",
		}),

		snapshotsPinnedSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "snapshots_pinned_size",
			Help:      "A running cumulative sum of the size of keys and values written to sstables during flushes or compactions that would've been elided if it weren't for open snapshots.",
		}),

		tableObsoleteSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_obsolete_size",
			Help:      "The number of bytes present in obsolete tables",
		}),

		tableObsoleteCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_obsolete_count",
			Help:      "The count of obsolete tables.",
		}),

		tableZombieSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_zombie_size",
			Help:      "the number of bytes present in zombie tables which are no longer referenced by the current DB state but are still in use by an iterator.",
		}),

		tableZombieCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "table_zombie_count",
			Help:      "The count of zombie tables.",
		}),

		walFiles: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "wal_files",
			Help:      "Number of live WAL files",
		}),

		walObsoleteFiles: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "wal_obsolete_files",
			Help:      "Number of obsolete WAL files.",
		}),

		walObsoletePhysicalSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "wal_obsolete_physicalsize",
			Help:      "Physical size of the obsolete WAL files",
		}),

		walSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "wal_size",
			Help:      "Size of the live data in the WAL files.",
		}),

		walPhysicalSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "wal_physicalsize",
			Help:      "Physical size of the WAL files on-disk.",
		}),

		walBytesIn: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "wal_bytes_in",
			Help:      "Number of logical bytes written to the WAL.",
		}),

		walBytesWritten: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "wal_bytes_written",
			Help:      "Number of bytes written to the WAL.",
		}),

		logwriterFsyncLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "logwrite_fsync_latency",
			Help:      "logwriter fsync latency",
		}),

		priorMetrics: &pebble.Metrics{},
	}

	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.compactEstimatedDebt),
		reg.Register(m.compactInProgressBytes),
		reg.Register(m.compactNumInProgress),
		reg.Register(m.compactMarkedFiles),

		reg.Register(m.flushCount),
		reg.Register(m.flushNumInProgress),
		reg.Register(m.flushAsIngestCount),
		reg.Register(m.flushAsIngestTableCount),
		reg.Register(m.flushAsIngestBytes),

		reg.Register(m.memtableSize),
		reg.Register(m.memtableCount),

		reg.Register(m.keysRangeKeySetsCount),
		reg.Register(m.keysTombstoneCount),

		reg.Register(m.snapshotsCount),
		reg.Register(m.snapshotsEarliestSeqNum),
		reg.Register(m.snapshotsPinnedKeys),
		reg.Register(m.snapshotsPinnedSize),

		reg.Register(m.tableObsoleteSize),
		reg.Register(m.tableObsoleteCount),
		reg.Register(m.tableZombieSize),
		reg.Register(m.tableZombieCount),

		reg.Register(m.walFiles),
		reg.Register(m.walObsoleteFiles),
		reg.Register(m.walObsoletePhysicalSize),
		reg.Register(m.walSize),
		reg.Register(m.walPhysicalSize),
		reg.Register(m.walBytesIn),
		reg.Register(m.walBytesWritten),

		reg.Register(m.logwriterFsyncLatency),
	)
	return m, errs.Err
}

func (db *Database) updateMetrics() {
	metrics := &db.metrics

	priorMetrics := metrics.priorMetrics

	// Retrieve the database stats
	currentMetrics := db.db.Metrics()
	metrics.compactEstimatedDebt.Add(float64(currentMetrics.Compact.EstimatedDebt - priorMetrics.Compact.EstimatedDebt))
	metrics.compactInProgressBytes.Add(float64(currentMetrics.Compact.InProgressBytes))
	metrics.compactInProgressBytes.Add(float64(currentMetrics.Compact.NumInProgress))
	metrics.compactInProgressBytes.Add(float64(currentMetrics.Compact.MarkedFiles))

	metrics.flushCount.Add(float64(currentMetrics.Flush.Count))
	metrics.flushNumInProgress.Add(float64(currentMetrics.Flush.NumInProgress))
	metrics.flushAsIngestCount.Add(float64(currentMetrics.Flush.AsIngestCount))
	metrics.flushAsIngestTableCount.Add(float64(currentMetrics.Flush.AsIngestTableCount))
	metrics.flushAsIngestBytes.Add(float64(currentMetrics.Flush.AsIngestBytes))

	metrics.memtableSize.Add(float64(currentMetrics.MemTable.Size))
	metrics.memtableCount.Add(float64(currentMetrics.MemTable.Count))
	metrics.memtableZombieSize.Add(float64(currentMetrics.MemTable.ZombieSize))
	metrics.memtableZombieCount.Add(float64(currentMetrics.MemTable.ZombieCount))

	metrics.keysRangeKeySetsCount.Add(float64(currentMetrics.Keys.RangeKeySetsCount))
	metrics.keysTombstoneCount.Add(float64(currentMetrics.Keys.TombstoneCount))

	metrics.snapshotsCount.Add(float64(currentMetrics.Snapshots.Count))
	metrics.snapshotsEarliestSeqNum.Add(float64(currentMetrics.Snapshots.EarliestSeqNum))
	metrics.snapshotsPinnedKeys.Add(float64(currentMetrics.Snapshots.PinnedKeys))
	metrics.snapshotsPinnedSize.Add(float64(currentMetrics.Snapshots.PinnedSize))

	metrics.tableObsoleteSize.Add(float64(currentMetrics.Table.ObsoleteSize))
	metrics.tableObsoleteCount.Add(float64(currentMetrics.Table.ObsoleteCount))
	metrics.tableZombieSize.Add(float64(currentMetrics.Table.ZombieSize))
	metrics.tableZombieCount.Add(float64(currentMetrics.Table.ZombieCount))

	metrics.walFiles.Add(float64(currentMetrics.WAL.Files))
	metrics.walObsoleteFiles.Add(float64(currentMetrics.WAL.ObsoleteFiles))
	metrics.walObsoletePhysicalSize.Add(float64(currentMetrics.WAL.ObsoletePhysicalSize))
	metrics.walSize.Add(float64(currentMetrics.WAL.Size))
	metrics.walPhysicalSize.Add(float64(currentMetrics.WAL.PhysicalSize))
	metrics.walBytesIn.Add(float64(currentMetrics.WAL.BytesIn))
	metrics.walBytesWritten.Add(float64(currentMetrics.WAL.BytesWritten))

	// metrics.logwriterFsyncLatency.histogram_quantile(currentMetrics.LogWriter.FsyncLatency)

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
}
