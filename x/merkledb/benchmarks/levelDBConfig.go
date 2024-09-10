// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"time"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type config struct {
	// BlockCacheCapacity defines the capacity of the 'sorted table' block caching.
	// Use -1 for zero, this has same effect as specifying NoCacher to BlockCacher.
	//
	// The default value is 12MiB.
	BlockCacheCapacity int `json:"blockCacheCapacity"`
	// BlockSize is the minimum uncompressed size in bytes of each 'sorted table'
	// block.
	//
	// The default value is 4KiB.
	BlockSize int `json:"blockSize"`
	// CompactionExpandLimitFactor limits compaction size after expanded.
	// This will be multiplied by table size limit at compaction target level.
	//
	// The default value is 25.
	CompactionExpandLimitFactor int `json:"compactionExpandLimitFactor"`
	// CompactionGPOverlapsFactor limits overlaps in grandparent (Level + 2)
	// that a single 'sorted table' generates.  This will be multiplied by
	// table size limit at grandparent level.
	//
	// The default value is 10.
	CompactionGPOverlapsFactor int `json:"compactionGPOverlapsFactor"`
	// CompactionL0Trigger defines number of 'sorted table' at level-0 that will
	// trigger compaction.
	//
	// The default value is 4.
	CompactionL0Trigger int `json:"compactionL0Trigger"`
	// CompactionSourceLimitFactor limits compaction source size. This doesn't apply to
	// level-0.
	// This will be multiplied by table size limit at compaction target level.
	//
	// The default value is 1.
	CompactionSourceLimitFactor int `json:"compactionSourceLimitFactor"`
	// CompactionTableSize limits size of 'sorted table' that compaction generates.
	// The limits for each level will be calculated as:
	//   CompactionTableSize * (CompactionTableSizeMultiplier ^ Level)
	// The multiplier for each level can also fine-tuned using CompactionTableSizeMultiplierPerLevel.
	//
	// The default value is 2MiB.
	CompactionTableSize int `json:"compactionTableSize"`
	// CompactionTableSizeMultiplier defines multiplier for CompactionTableSize.
	//
	// The default value is 1.
	CompactionTableSizeMultiplier float64 `json:"compactionTableSizeMultiplier"`
	// CompactionTableSizeMultiplierPerLevel defines per-level multiplier for
	// CompactionTableSize.
	// Use zero to skip a level.
	//
	// The default value is nil.
	CompactionTableSizeMultiplierPerLevel []float64 `json:"compactionTableSizeMultiplierPerLevel"`
	// CompactionTotalSize limits total size of 'sorted table' for each level.
	// The limits for each level will be calculated as:
	//   CompactionTotalSize * (CompactionTotalSizeMultiplier ^ Level)
	// The multiplier for each level can also fine-tuned using
	// CompactionTotalSizeMultiplierPerLevel.
	//
	// The default value is 10MiB.
	CompactionTotalSize int `json:"compactionTotalSize"`
	// CompactionTotalSizeMultiplier defines multiplier for CompactionTotalSize.
	//
	// The default value is 10.
	CompactionTotalSizeMultiplier float64 `json:"compactionTotalSizeMultiplier"`
	// DisableSeeksCompaction allows disabling 'seeks triggered compaction'.
	// The purpose of 'seeks triggered compaction' is to optimize database so
	// that 'level seeks' can be minimized, however this might generate many
	// small compaction which may not preferable.
	//
	// The default is true.
	DisableSeeksCompaction bool `json:"disableSeeksCompaction"`
	// OpenFilesCacheCapacity defines the capacity of the open files caching.
	// Use -1 for zero, this has same effect as specifying NoCacher to OpenFilesCacher.
	//
	// The default value is 1024.
	OpenFilesCacheCapacity int `json:"openFilesCacheCapacity"`
	// WriteBuffer defines maximum size of a 'memdb' before flushed to
	// 'sorted table'. 'memdb' is an in-memory DB backed by an on-disk
	// unsorted journal.
	//
	// LevelDB may held up to two 'memdb' at the same time.
	//
	// The default value is 6MiB.
	WriteBuffer      int `json:"writeBuffer"`
	FilterBitsPerKey int `json:"filterBitsPerKey"`

	// MaxManifestFileSize is the maximum size limit of the MANIFEST-****** file.
	// When the MANIFEST-****** file grows beyond this size, LevelDB will create
	// a new MANIFEST file.
	//
	// The default value is infinity.
	MaxManifestFileSize int64 `json:"maxManifestFileSize"`

	// MetricUpdateFrequency is the frequency to poll LevelDB metrics.
	// If <= 0, LevelDB metrics aren't polled.
	MetricUpdateFrequency time.Duration `json:"metricUpdateFrequency"`
}

func getLevelDbConfig() []byte {
	parsedConfig := config{
		BlockCacheCapacity:     16 * opt.MiB,
		DisableSeeksCompaction: true,
		OpenFilesCacheCapacity: 64,
		WriteBuffer:            256 * opt.MiB,
		FilterBitsPerKey:       leveldb.DefaultBitsPerKey,
		MaxManifestFileSize:    leveldb.DefaultMaxManifestFileSize,
		MetricUpdateFrequency:  leveldb.DefaultMetricUpdateFrequency,
	}
	bytes, _ := json.Marshal(parsedConfig)
	return bytes
}
