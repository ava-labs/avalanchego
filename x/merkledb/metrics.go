// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
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
	ViewChangesNodeHit()
	ViewChangesNodeMiss()
	ViewChangesValueHit()
	ViewChangesValueMiss()
}

type mockMetrics struct {
	lock                      sync.Mutex
	keyReadCount              int64
	keyWriteCount             int64
	hashCount                 int64
	valueNodeCacheHit         int64
	valueNodeCacheMiss        int64
	intermediateNodeCacheHit  int64
	intermediateNodeCacheMiss int64
	viewNodeCacheHit          int64
	viewChangesNodeMiss       int64
	viewChangesValueHit       int64
	viewChangesValueMiss      int64
}

func (m *mockMetrics) HashCalculated() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.hashCount++
}

func (m *mockMetrics) DatabaseNodeRead() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.keyReadCount++
}

func (m *mockMetrics) DatabaseNodeWrite() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.keyWriteCount++
}

func (m *mockMetrics) ViewChangesNodeHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewNodeCacheHit++
}

func (m *mockMetrics) ViewChangesValueHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesValueHit++
}

func (m *mockMetrics) ViewChangesNodeMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesNodeMiss++
}

func (m *mockMetrics) ViewChangesValueMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.viewChangesValueMiss++
}

func (m *mockMetrics) ValueNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.valueNodeCacheHit++
}

func (m *mockMetrics) ValueNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.valueNodeCacheMiss++
}

func (m *mockMetrics) IntermediateNodeCacheHit() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.intermediateNodeCacheHit++
}

func (m *mockMetrics) IntermediateNodeCacheMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.intermediateNodeCacheMiss++
}

type metrics struct {
	ioKeyWrite                prometheus.Counter
	ioKeyRead                 prometheus.Counter
	hashCount                 prometheus.Counter
	intermediateNodeCacheHit  prometheus.Counter
	intermediateNodeCacheMiss prometheus.Counter
	valueNodeCacheHit         prometheus.Counter
	valueNodeCacheMiss        prometheus.Counter
	viewChangesNodeHit        prometheus.Counter
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
		viewChangesNodeHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_changes_node_hit",
			Help:      "cumulative amount of hits looking up a node in a view's change set",
		}),
		viewNodeCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_changes_node_miss",
			Help:      "cumulative amount of misses looking up a node in a view's change set",
		}),
		viewValueCacheHit: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_changes_value_hit",
			Help:      "cumulative amount of hits looking up a value in a view's change set",
		}),
		viewValueCacheMiss: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "view_changes_value_miss",
			Help:      "cumulative amount of misses looking up a value in a view's change set",
		}),
	}
	err := utils.Err(
		reg.Register(m.ioKeyWrite),
		reg.Register(m.ioKeyRead),
		reg.Register(m.hashCount),
		reg.Register(m.valueNodeCacheHit),
		reg.Register(m.valueNodeCacheMiss),
		reg.Register(m.intermediateNodeCacheHit),
		reg.Register(m.intermediateNodeCacheMiss),
		reg.Register(m.viewChangesNodeHit),
		reg.Register(m.viewNodeCacheMiss),
		reg.Register(m.viewValueCacheHit),
		reg.Register(m.viewValueCacheMiss),
	)
	return &m, err
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

func (m *metrics) ViewChangesNodeHit() {
	m.viewChangesNodeHit.Inc()
}

func (m *metrics) ViewChangesNodeMiss() {
	m.viewNodeCacheMiss.Inc()
}

func (m *metrics) ViewChangesValueHit() {
	m.viewValueCacheHit.Inc()
}

func (m *metrics) ViewChangesValueMiss() {
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
