// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ merkleMetrics = &mockMetrics{}
	_ merkleMetrics = &metrics{}
)

type merkleMetrics interface {
	IOKeyRead()
	IOKeyWrite()
	HashCalculated()
	DBNodeCacheHit()
	DBNodeCacheMiss()
	ViewNodeCacheHit()
	ViewNodeCacheMiss()
	ViewValueCacheHit()
	ViewValueCacheMiss()
}

type mockMetrics struct {
	lock               sync.Mutex
	keyReadCount       int64
	keyWriteCount      int64
	hashCount          int64
	dbNodeCacheHit     int64
	dbNodeCacheMiss    int64
	viewNodeCacheHit   int64
	viewNodeCacheMiss  int64
	viewValueCacheHit  int64
	viewValueCacheMiss int64
}

func (m *mockMetrics) HashCalculated() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.hashCount++
}

func (m *mockMetrics) IOKeyRead() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.keyReadCount++
}

func (m *mockMetrics) IOKeyWrite() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.keyWriteCount++
}

func (m *mockMetrics) ViewNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewNodeCacheHit++
}

func (m *mockMetrics) ViewValueCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewValueCacheHit++
}

func (m *mockMetrics) ViewNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewNodeCacheMiss++
}

func (m *mockMetrics) ViewValueCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewValueCacheMiss++
}

func (m *mockMetrics) DBNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.dbNodeCacheHit++
}

func (m *mockMetrics) DBNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.dbNodeCacheMiss++
}

type metrics struct {
	ioKeyWrite         prometheus.Counter
	ioKeyRead          prometheus.Counter
	hashCount          prometheus.Counter
	dbNodeCacheHit     prometheus.Counter
	dbNodeCacheMiss    prometheus.Counter
	viewNodeCacheHit   prometheus.Counter
	viewNodeCacheMiss  prometheus.Counter
	viewValueCacheHit  prometheus.Counter
	viewValueCacheMiss prometheus.Counter
}

func newMetrics(namespace string, reg prometheus.Registerer) (merkleMetrics, error) {
	if reg == nil {
		return &mockMetrics{}, nil
	}
	m := metrics{
		ioKeyWrite: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "io_key_write",
			Help:      "cumulative amount of io write to the key db",
		}),
		ioKeyRead: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "io_key_read",
			Help:      "cumulative amount of io read to the key db",
		}),
		hashCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "hashes_calculated",
			Help:      "cumulative number of node hashes done",
		}),
		dbNodeCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "db_node_cache_hit",
			Help:      "cumulative amount of hits on the db node cache",
		}),
		dbNodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "db_node_cache_miss",
			Help:      "cumulative amount of misses on the db node cache",
		}),
		viewNodeCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_node_cache_hit",
			Help:      "cumulative amount of hits on the view node cache",
		}),
		viewNodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_node_cache_miss",
			Help:      "cumulative amount of misses on the view node cache",
		}),
		viewValueCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_value_cache_hit",
			Help:      "cumulative amount of hits on the view value cache",
		}),
		viewValueCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_value_cache_miss",
			Help:      "cumulative amount of misses on the view value cache",
		}),
	}
	errs := wrappers.Errs{}
	errs.Add(
		reg.Register(m.ioKeyWrite),
		reg.Register(m.ioKeyRead),
		reg.Register(m.hashCount),
		reg.Register(m.dbNodeCacheHit),
		reg.Register(m.dbNodeCacheMiss),
		reg.Register(m.viewNodeCacheHit),
		reg.Register(m.viewNodeCacheMiss),
		reg.Register(m.viewValueCacheHit),
		reg.Register(m.viewValueCacheMiss),
	)
	return &m, errs.Err
}

func (m *metrics) IOKeyRead() {
	m.ioKeyRead.Inc()
}

func (m *metrics) IOKeyWrite() {
	m.ioKeyWrite.Inc()
}

func (m *metrics) HashCalculated() {
	m.hashCount.Inc()
}

func (m *metrics) ViewNodeCacheHit() {
	m.viewNodeCacheHit.Inc()
}

func (m *metrics) ViewNodeCacheMiss() {
	m.viewNodeCacheMiss.Inc()
}

func (m *metrics) ViewValueCacheHit() {
	m.viewValueCacheHit.Inc()
}

func (m *metrics) ViewValueCacheMiss() {
	m.viewValueCacheMiss.Inc()
}

func (m *metrics) DBNodeCacheHit() {
	m.dbNodeCacheHit.Inc()
}

func (m *metrics) DBNodeCacheMiss() {
	m.dbNodeCacheMiss.Inc()
}
