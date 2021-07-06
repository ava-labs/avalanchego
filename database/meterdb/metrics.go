// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func newSizeMetric(namespace, name string) prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_size", name),
		Help:      fmt.Sprintf("Bytes passed in a %s call", name),
		Buckets:   metric.BytesBuckets,
	})
}

type metrics struct {
	readSize,
	writeSize,
	has,
	hasSize,
	get,
	getSize,
	put,
	putSize,
	delete,
	deleteSize,
	newBatch,
	newIterator,
	stat,
	compact,
	close,
	bPut,
	bPutSize,
	bDelete,
	bDeleteSize,
	bSize,
	bWrite,
	bWriteSize,
	bReset,
	bReplay,
	bInner,
	iNext,
	iNextSize,
	iError,
	iKey,
	iValue,
	iRelease prometheus.Histogram
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.readSize = newSizeMetric(namespace, "read")
	m.writeSize = newSizeMetric(namespace, "write")
	m.has = metric.NewNanosecondsLatencyMetric(namespace, "has")
	m.hasSize = newSizeMetric(namespace, "has")
	m.get = metric.NewNanosecondsLatencyMetric(namespace, "get")
	m.getSize = newSizeMetric(namespace, "get")
	m.put = metric.NewNanosecondsLatencyMetric(namespace, "put")
	m.putSize = newSizeMetric(namespace, "put")
	m.delete = metric.NewNanosecondsLatencyMetric(namespace, "delete")
	m.deleteSize = newSizeMetric(namespace, "delete")
	m.newBatch = metric.NewNanosecondsLatencyMetric(namespace, "new_batch")
	m.newIterator = metric.NewNanosecondsLatencyMetric(namespace, "new_iterator")
	m.stat = metric.NewNanosecondsLatencyMetric(namespace, "stat")
	m.compact = metric.NewNanosecondsLatencyMetric(namespace, "compact")
	m.close = metric.NewNanosecondsLatencyMetric(namespace, "close")
	m.bPut = metric.NewNanosecondsLatencyMetric(namespace, "batch_put")
	m.bPutSize = newSizeMetric(namespace, "batch_put")
	m.bDelete = metric.NewNanosecondsLatencyMetric(namespace, "batch_delete")
	m.bDeleteSize = newSizeMetric(namespace, "batch_delete")
	m.bSize = metric.NewNanosecondsLatencyMetric(namespace, "batch_size")
	m.bWrite = metric.NewNanosecondsLatencyMetric(namespace, "batch_write")
	m.bWriteSize = newSizeMetric(namespace, "batch_write")
	m.bReset = metric.NewNanosecondsLatencyMetric(namespace, "batch_reset")
	m.bReplay = metric.NewNanosecondsLatencyMetric(namespace, "batch_replay")
	m.bInner = metric.NewNanosecondsLatencyMetric(namespace, "batch_inner")
	m.iNext = metric.NewNanosecondsLatencyMetric(namespace, "iterator_next")
	m.iNextSize = newSizeMetric(namespace, "iterator_next")
	m.iError = metric.NewNanosecondsLatencyMetric(namespace, "iterator_error")
	m.iKey = metric.NewNanosecondsLatencyMetric(namespace, "iterator_key")
	m.iValue = metric.NewNanosecondsLatencyMetric(namespace, "iterator_value")
	m.iRelease = metric.NewNanosecondsLatencyMetric(namespace, "iterator_release")

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.readSize),
		registerer.Register(m.writeSize),
		registerer.Register(m.has),
		registerer.Register(m.hasSize),
		registerer.Register(m.get),
		registerer.Register(m.getSize),
		registerer.Register(m.put),
		registerer.Register(m.putSize),
		registerer.Register(m.delete),
		registerer.Register(m.deleteSize),
		registerer.Register(m.newBatch),
		registerer.Register(m.newIterator),
		registerer.Register(m.stat),
		registerer.Register(m.compact),
		registerer.Register(m.close),
		registerer.Register(m.bPut),
		registerer.Register(m.bPutSize),
		registerer.Register(m.bDelete),
		registerer.Register(m.bDeleteSize),
		registerer.Register(m.bSize),
		registerer.Register(m.bWrite),
		registerer.Register(m.bWriteSize),
		registerer.Register(m.bReset),
		registerer.Register(m.bReplay),
		registerer.Register(m.bInner),
		registerer.Register(m.iNext),
		registerer.Register(m.iNextSize),
		registerer.Register(m.iError),
		registerer.Register(m.iKey),
		registerer.Register(m.iValue),
		registerer.Register(m.iRelease),
	)
	return errs.Err
}
