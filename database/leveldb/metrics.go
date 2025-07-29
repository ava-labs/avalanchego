// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"errors"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/syndtr/goleveldb/leveldb"
)

var levelLabels = []string{"level"}

type metrics struct {
	// total number of writes that have been delayed due to compaction
	writesDelayedCount prometheus.Counter
	// total amount of time (in ns) that writes that have been delayed due to
	// compaction
	writesDelayedDuration prometheus.Gauge
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
	levelDuration *prometheus.GaugeVec
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
}

func newMetrics(reg prometheus.Registerer) (metrics, error) {
	m := metrics{
		writesDelayedCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "writes_delayed",
			Help: "number of cumulative writes that have been delayed due to compaction",
		}),
		writesDelayedDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "writes_delayed_duration",
			Help: "amount of time (in ns) that writes have been delayed due to compaction",
		}),
		writeIsDelayed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "write_delayed",
			Help: "1 if there is currently a write that is being delayed due to compaction",
		}),

		aliveSnapshots: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "alive_snapshots",
			Help: "number of currently alive snapshots",
		}),
		aliveIterators: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "alive_iterators",
			Help: "number of currently alive iterators",
		}),

		ioWrite: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "io_write",
			Help: "cumulative amount of io write during compaction",
		}),
		ioRead: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "io_read",
			Help: "cumulative amount of io read during compaction",
		}),

		blockCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "block_cache_size",
			Help: "total size of cached blocks",
		}),
		openTables: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "open_tables",
			Help: "number of currently opened tables",
		}),

		levelTableCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "table_count",
				Help: "number of tables allocated by level",
			},
			levelLabels,
		),
		levelSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "size",
				Help: "amount of bytes allocated by level",
			},
			levelLabels,
		),
		levelDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "duration",
				Help: "amount of time (in ns) spent in compaction by level",
			},
			levelLabels,
		),
		levelReads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "reads",
				Help: "amount of bytes read during compaction by level",
			},
			levelLabels,
		),
		levelWrites: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "writes",
				Help: "amount of bytes written during compaction by level",
			},
			levelLabels,
		),

		memCompactions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "mem_comps",
			Help: "total number of memory compactions performed",
		}),
		level0Compactions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "level_0_comps",
			Help: "total number of level 0 compactions performed",
		}),
		nonLevel0Compactions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "non_level_0_comps",
			Help: "total number of non-level 0 compactions performed",
		}),
		seekCompactions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "seek_comps",
			Help: "total number of seek compactions performed",
		}),

		priorStats:   &leveldb.DBStats{},
		currentStats: &leveldb.DBStats{},
	}

	err := errors.Join(
		reg.Register(m.writesDelayedCount),
		reg.Register(m.writesDelayedDuration),
		reg.Register(m.writeIsDelayed),

		reg.Register(m.aliveSnapshots),
		reg.Register(m.aliveIterators),

		reg.Register(m.ioWrite),
		reg.Register(m.ioRead),

		reg.Register(m.blockCacheSize),
		reg.Register(m.openTables),

		reg.Register(m.levelTableCount),
		reg.Register(m.levelSize),
		reg.Register(m.levelDuration),
		reg.Register(m.levelReads),
		reg.Register(m.levelWrites),

		reg.Register(m.memCompactions),
		reg.Register(m.level0Compactions),
		reg.Register(m.nonLevel0Compactions),
		reg.Register(m.seekCompactions),
	)
	return m, err
}

func (db *Database) updateMetrics() error {
	metrics := &db.metrics

	priorStats := metrics.priorStats
	currentStats := metrics.currentStats

	// Retrieve the database stats
	if err := db.DB.Stats(currentStats); err != nil {
		return err
	}

	metrics.writesDelayedCount.Add(float64(currentStats.WriteDelayCount - priorStats.WriteDelayCount))
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

	// update the priorStats to update the counters correctly next time this
	// method is called
	metrics.priorStats = currentStats

	// update currentStats to a pre-allocated stats struct. This avoids
	// performing memory allocations for each update
	metrics.currentStats = priorStats
	return nil
}
