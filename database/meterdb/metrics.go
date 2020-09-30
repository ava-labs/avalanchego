// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func newMetric(namespace, name string) prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      name,
		Help:      fmt.Sprintf("Latency of a %s call in nanoseconds", name),
		Buckets:   timer.NanosecondsBuckets,
	})
}

type metrics struct {
	has,
	get,
	put,
	delete,
	newBatch,
	newIterator,
	stat,
	compact,
	close,
	bPut,
	bDelete,
	bValueSize,
	bWrite,
	bReset,
	bReplay,
	bInner,
	iNext,
	iError,
	iKey,
	iValue,
	iRelease prometheus.Histogram
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.has = newMetric(namespace, "has")
	m.get = newMetric(namespace, "get")
	m.put = newMetric(namespace, "put")
	m.delete = newMetric(namespace, "delete")
	m.newBatch = newMetric(namespace, "new_batch")
	m.newIterator = newMetric(namespace, "new_iterator")
	m.stat = newMetric(namespace, "stat")
	m.compact = newMetric(namespace, "compact")
	m.close = newMetric(namespace, "close")
	m.bPut = newMetric(namespace, "batch_put")
	m.bDelete = newMetric(namespace, "batch_delete")
	m.bValueSize = newMetric(namespace, "batch_value_size")
	m.bWrite = newMetric(namespace, "batch_write")
	m.bReset = newMetric(namespace, "batch_reset")
	m.bReplay = newMetric(namespace, "batch_replay")
	m.bInner = newMetric(namespace, "batch_inner")
	m.iNext = newMetric(namespace, "iterator_next")
	m.iError = newMetric(namespace, "iterator_error")
	m.iKey = newMetric(namespace, "iterator_key")
	m.iValue = newMetric(namespace, "iterator_value")
	m.iRelease = newMetric(namespace, "iterator_release")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.has),
		registerer.Register(m.get),
		registerer.Register(m.put),
		registerer.Register(m.delete),
		registerer.Register(m.newBatch),
		registerer.Register(m.newIterator),
		registerer.Register(m.stat),
		registerer.Register(m.compact),
		registerer.Register(m.close),
		registerer.Register(m.bPut),
		registerer.Register(m.bDelete),
		registerer.Register(m.bValueSize),
		registerer.Register(m.bWrite),
		registerer.Register(m.bReset),
		registerer.Register(m.bReplay),
		registerer.Register(m.bInner),
		registerer.Register(m.iNext),
		registerer.Register(m.iError),
		registerer.Register(m.iKey),
		registerer.Register(m.iValue),
		registerer.Register(m.iRelease),
	)
	return errs.Err
}
