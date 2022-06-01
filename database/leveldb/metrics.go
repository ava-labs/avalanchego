// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leveldb

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var levelLabels = []string{"level"}

// All of the metrics need
type metrics struct {
	writesDelayedCount    prometheus.Counter // cumulative
	writesDelayedDuration prometheus.Counter // cumulative
	writeIsDelayed        prometheus.Gauge

	aliveSnapshots prometheus.Gauge
	aliveIterators prometheus.Gauge

	ioWrite prometheus.Counter // cumulative
	ioRead  prometheus.Counter // cumulative

	blockCacheSize prometheus.Gauge
	openTables     prometheus.Gauge

	levelTableCount *prometheus.GaugeVec
	levelSize       *prometheus.GaugeVec
	levelDuration   *prometheus.CounterVec // cumulative
	levelReads      *prometheus.CounterVec // cumulative
	levelWrites     *prometheus.CounterVec // cumulative

	memCompactions       prometheus.Counter // cumulative
	level0Compactions    prometheus.Counter // cumulative
	nonLevel0Compactions prometheus.Counter // cumulative
	seekCompactions      prometheus.Counter // cumulative

	priorIndex int
	stats      [2]*leveldb.DBStats
}

func newMetrics(namespace string, reg prometheus.Registerer) (metrics, error) {
	m := metrics{
		writesDelayedCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "writes_delayed",
			Help:      "number of cumulative writes that have been delayed due to compaction",
		}),
		writesDelayedDuration: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "writes_delayed_duration",
			Help:      "amount of time (in ns) that writes have been delayed due to compaction",
		}),
		writeIsDelayed: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "write_delayed",
			Help:      "1 if there is currently a write that is being delayed due to compaction",
		}),

		aliveSnapshots: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "alive_snapshots",
			Help:      "number of currently alive snapshots",
		}),
		aliveIterators: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "alive_iterators",
			Help:      "number of currently alive iterators",
		}),

		ioWrite: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "io_write",
			Help:      "cumulative amount of io write during compaction",
		}),
		ioRead: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "io_read",
			Help:      "cumulative amount of io read during compaction",
		}),

		blockCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "block_cache_size",
			Help:      "total size of cached blocks",
		}),
		openTables: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "open_tables",
			Help:      "number of currently opened tables",
		}),

		levelTableCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "table_count",
				Help:      "number of tables allocated by level",
			},
			levelLabels,
		),
		levelSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "size",
				Help:      "amount of bytes allocated by level",
			},
			levelLabels,
		),
		levelDuration: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "duration",
				Help:      "amount of time (in ns) spent in compaction by level",
			},
			levelLabels,
		),
		levelReads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "reads",
				Help:      "amount of bytes read during compaction by level",
			},
			levelLabels,
		),
		levelWrites: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "writes",
				Help:      "amount of bytes written during compaction by level",
			},
			levelLabels,
		),

		memCompactions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "mem_comps",
			Help:      "total number of memory compactions performed",
		}),
		level0Compactions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "level_0_comps",
			Help:      "total number of level 0 compactions performed",
		}),
		nonLevel0Compactions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "non_level_0_comps",
			Help:      "total number of non-level 0 compactions performed",
		}),
		seekCompactions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "seek_comps",
			Help:      "total number of seek compactions performed",
		}),

		stats: [2]*leveldb.DBStats{{}, {}},
	}

	errs := wrappers.Errs{}
	errs.Add(
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
	return m, errs.Err
}

func (db *Database) updateMetrics() error {
	metrics := &db.metrics

	priorIndex := metrics.priorIndex
	currentIndex := 1 - priorIndex
	metrics.priorIndex = currentIndex

	currentStats := metrics.stats[currentIndex]
	priorStats := metrics.stats[priorIndex]

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
	return nil
}
