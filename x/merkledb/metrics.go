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
	ValueNodeCacheHit()
	ValueNodeCacheMiss()
	IntermediateNodeCacheHit()
	IntermediateNodeCacheMiss()
	ViewNodeCacheHit()
	ViewNodeCacheMiss()
	ViewValueCacheHit()
	ViewValueCacheMiss()
}

type mockMetrics struct {
	keyReadCount              int64
	keyWriteCount             int64
	hashCount                 int64
	valueNodeCacheHit         int64
	valueNodeCacheMiss        int64
	intermediateNodeCacheHit  int64
	intermediateNodeCacheMiss int64
	viewNodeCacheHit          int64
	viewNodeCacheMiss         int64
	viewValueCacheHit         int64
	viewValueCacheMiss        int64
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

func (m *mockMetrics) ValueNodeCacheHit() {
	atomic.AddInt64(&m.valueNodeCacheHit, 1)
}

func (m *mockMetrics) ValueNodeCacheMiss() {
	atomic.AddInt64(&m.valueNodeCacheMiss, 1)
}

func (m *mockMetrics) IntermediateNodeCacheHit() {
	atomic.AddInt64(&m.intermediateNodeCacheHit, 1)
}

func (m *mockMetrics) IntermediateNodeCacheMiss() {
	atomic.AddInt64(&m.intermediateNodeCacheMiss, 1)
}

type metrics struct {
	ioKeyWrite                prometheus.Counter
	ioKeyRead                 prometheus.Counter
	hashCount                 prometheus.Counter
	intermediateNodeCacheHit  prometheus.Counter
	intermediateNodeCacheMiss prometheus.Counter
	valueNodeCacheHit         prometheus.Counter
	valueNodeCacheMiss        prometheus.Counter
	viewNodeCacheHit          prometheus.Counter
	viewNodeCacheMiss         prometheus.Counter
	viewValueCacheHit         prometheus.Counter
	viewValueCacheMiss        prometheus.Counter
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
		valueNodeCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "value_node_cache_hit",
			Help:      "cumulative amount of hits on the value node db cache",
		}),
		valueNodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "value_node_cache_miss",
			Help:      "cumulative amount of misses on the value node db cache",
		}),
		intermediateNodeCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "intermediate_node_cache_hit",
			Help:      "cumulative amount of hits on the intermediate node db cache",
		}),
		intermediateNodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
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
		reg.Register(m.valueNodeCacheHit),
		reg.Register(m.valueNodeCacheMiss),
		reg.Register(m.intermediateNodeCacheHit),
		reg.Register(m.intermediateNodeCacheMiss),
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

func (m *metrics) IntermediateNodeCacheHit() {
	m.intermediateNodeCacheHit.Inc()
}

func (m *metrics) IntermediateNodeCacheMiss() {
	m.intermediateNodeCacheMiss.Inc()
}

func (m *metrics) ValueNodeCacheHit() {
	m.valueNodeCacheHit.Inc()
}

func (m *metrics) ValueNodeCacheMiss() {
	m.valueNodeCacheMiss.Inc()
}
