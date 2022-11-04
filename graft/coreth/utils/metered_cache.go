// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// MeteredCache wraps *fastcache.Cache and periodically pulls stats from it.
type MeteredCache struct {
	*fastcache.Cache
	namespace string

	// stats to be surfaced
	entriesCount metrics.Gauge
	bytesSize    metrics.Gauge
	collisions   metrics.Gauge
	gets         metrics.Gauge
	sets         metrics.Gauge
	misses       metrics.Gauge
	statsTime    metrics.Gauge

	// count all operations to decide when to update stats
	ops             uint64
	updateFrequency uint64
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// NewMeteredCache returns a new MeteredCache that will update stats to the
// provided namespace once per each [updateFrequency] operations.
// Note: if [updateFrequency] is passed as 0, it will be treated as 1.
func NewMeteredCache(size int, journal string, namespace string, updateFrequency uint64) *MeteredCache {
	var cache *fastcache.Cache
	if journal == "" {
		cache = fastcache.New(size)
	} else {
		dirSize, err := dirSize(journal)
		log.Info("attempting to load cache from disk", "path", journal, "dirSize", common.StorageSize(dirSize), "err", err)
		cache = fastcache.LoadFromFileOrNew(journal, size)
	}
	if updateFrequency == 0 {
		updateFrequency = 1 // avoid division by zero
	}
	mc := &MeteredCache{
		Cache:           cache,
		namespace:       namespace,
		updateFrequency: updateFrequency,
	}
	if namespace != "" {
		// only register stats if a namespace is provided.
		mc.entriesCount = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/entriesCount", namespace), nil)
		mc.bytesSize = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/bytesSize", namespace), nil)
		mc.collisions = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/collisions", namespace), nil)
		mc.gets = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/gets", namespace), nil)
		mc.sets = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/sets", namespace), nil)
		mc.misses = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/misses", namespace), nil)
		mc.statsTime = metrics.GetOrRegisterGauge(fmt.Sprintf("%s/statsTime", namespace), nil)
	}
	return mc
}

// updateStats updates metrics from fastcache
func (mc *MeteredCache) updateStatsIfNeeded() {
	if mc.namespace == "" {
		return
	}
	ops := atomic.AddUint64(&mc.ops, 1)
	if ops%mc.updateFrequency != 0 {
		return
	}

	start := time.Now()
	s := fastcache.Stats{}
	mc.UpdateStats(&s)
	mc.entriesCount.Update(int64(s.EntriesCount))
	mc.bytesSize.Update(int64(s.BytesSize))
	mc.collisions.Update(int64(s.Collisions))
	mc.gets.Update(int64(s.GetCalls))
	mc.sets.Update(int64(s.SetCalls))
	mc.misses.Update(int64(s.Misses))
	mc.statsTime.Inc(int64(time.Since(start))) // cumulative metric
}

func (mc *MeteredCache) Del(k []byte) {
	mc.updateStatsIfNeeded()
	mc.Cache.Del(k)
}

func (mc *MeteredCache) Get(dst, k []byte) []byte {
	mc.updateStatsIfNeeded()
	return mc.Cache.Get(dst, k)
}

func (mc *MeteredCache) GetBig(dst, k []byte) []byte {
	mc.updateStatsIfNeeded()
	return mc.Cache.GetBig(dst, k)
}

func (mc *MeteredCache) Has(k []byte) bool {
	mc.updateStatsIfNeeded()
	return mc.Cache.Has(k)
}

func (mc *MeteredCache) HasGet(dst, k []byte) ([]byte, bool) {
	mc.updateStatsIfNeeded()
	return mc.Cache.HasGet(dst, k)
}

func (mc *MeteredCache) Set(k, v []byte) {
	mc.updateStatsIfNeeded()
	mc.Cache.Set(k, v)
}

func (mc *MeteredCache) SetBig(k, v []byte) {
	mc.updateStatsIfNeeded()
	mc.Cache.SetBig(k, v)
}
