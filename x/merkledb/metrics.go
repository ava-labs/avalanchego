// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/prometheus/client_golang/prometheus"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ merkleMetrics = (*mockMetrics)(nil)
	_ merkleMetrics = (*metrics)(nil)
)

type merkleMetrics interface {
	DatabaseNodeRead()
	DatabaseNodeWrite()
	HashCalculated()
	ValueCacheHit()
	ValueCacheMiss()
	NodeCacheHit()
	NodeCacheMiss()
	ViewNodeCacheHit()
	ViewNodeCacheMiss()
	ViewValueCacheHit()
	ViewValueCacheMiss()
}

type mockMetrics struct {
	keyReadCount       int64
	keyWriteCount      int64
	hashCount          int64
	valueCacheHit      int64
	valueCacheMiss     int64
	nodeCacheHit       int64
	nodeCacheMiss      int64
	viewNodeCacheHit   int64
	viewNodeCacheMiss  int64
	viewValueCacheHit  int64
	viewValueCacheMiss int64
}

func (m *mockMetrics) HashCalculated() {
	atomic.AddInt64(&m.hashCount, 1)
}

func (m *mockMetrics) DatabaseNodeRead() {
	atomic.AddInt64(&m.keyReadCount, 1)
}

func (m *mockMetrics) DatabaseNodeWrite() {
	atomic.AddInt64(&m.keyWriteCount, 1)
}

func (m *mockMetrics) ViewNodeCacheHit() {
	atomic.AddInt64(&m.viewNodeCacheHit, 1)
}

func (m *mockMetrics) ViewValueCacheHit() {
	atomic.AddInt64(&m.viewValueCacheHit, 1)
}

func (m *mockMetrics) ViewNodeCacheMiss() {
	atomic.AddInt64(&m.viewNodeCacheMiss, 1)
}

func (m *mockMetrics) ViewValueCacheMiss() {
	atomic.AddInt64(&m.viewValueCacheMiss, 1)
}

func (m *mockMetrics) ValueCacheHit() {
	atomic.AddInt64(&m.valueCacheHit, 1)
}

func (m *mockMetrics) ValueCacheMiss() {
	atomic.AddInt64(&m.valueCacheMiss, 1)
}

func (m *mockMetrics) NodeCacheHit() {
	atomic.AddInt64(&m.nodeCacheHit, 1)
}

func (m *mockMetrics) NodeCacheMiss() {
	atomic.AddInt64(&m.nodeCacheMiss, 1)
}

type metrics struct {
	ioKeyWrite         prometheus.Counter
	ioKeyRead          prometheus.Counter
	hashCount          prometheus.Counter
	nodeCacheHit       prometheus.Counter
	nodeCacheMiss      prometheus.Counter
	valueCacheHit      prometheus.Counter
	valueCacheMiss     prometheus.Counter
	viewNodeCacheHit   prometheus.Counter
	viewNodeCacheMiss  prometheus.Counter
	viewValueCacheHit  prometheus.Counter
	viewValueCacheMiss prometheus.Counter
}

func newMetrics(namespace string, reg prometheus.Registerer) (merkleMetrics, error) {
	// TODO: Should we instead return an error if reg is nil?
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
		valueCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "value_node_cache_hit",
			Help:      "cumulative amount of hits on the value node db cache",
		}),
		valueCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "value_node_cache_miss",
			Help:      "cumulative amount of misses on the value node db cache",
		}),
		nodeCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "intermediate_node_cache_hit",
			Help:      "cumulative amount of hits on the intermediate node db cache",
		}),
		nodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "intermediate_node_cache_miss",
			Help:      "cumulative amount of misses on the intermediate node db cache",
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
		reg.Register(m.valueCacheHit),
		reg.Register(m.valueCacheMiss),
		reg.Register(m.nodeCacheHit),
		reg.Register(m.nodeCacheMiss),
		reg.Register(m.viewNodeCacheHit),
		reg.Register(m.viewNodeCacheMiss),
		reg.Register(m.viewValueCacheHit),
		reg.Register(m.viewValueCacheMiss),
	)
	return &m, errs.Err
}

func (m *metrics) DatabaseNodeRead() {
	m.ioKeyRead.Inc()
}

func (m *metrics) DatabaseNodeWrite() {
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

func (m *metrics) NodeCacheHit() {
	m.nodeCacheHit.Inc()
}

func (m *metrics) NodeCacheMiss() {
	m.nodeCacheMiss.Inc()
}

func (m *metrics) ValueCacheHit() {
	m.valueCacheHit.Inc()
}

func (m *metrics) ValueCacheMiss() {
	m.valueCacheMiss.Inc()
}
