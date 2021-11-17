// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meterdb

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func newSizeMetric(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		fmt.Sprintf("%s_size", name),
		fmt.Sprintf("bytes passed in a %s call", name),
		reg,
		errs,
	)
}

func newTimeMetric(namespace, name string, reg prometheus.Registerer, errs *wrappers.Errs) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		name,
		fmt.Sprintf("time (in ns) of a %s", name),
		reg,
		errs,
	)
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
	iRelease metric.Averager
}

func (m *metrics) Initialize(
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.readSize = newSizeMetric(namespace, "read", reg, &errs)
	m.writeSize = newSizeMetric(namespace, "write", reg, &errs)
	m.has = newTimeMetric(namespace, "has", reg, &errs)
	m.hasSize = newSizeMetric(namespace, "has", reg, &errs)
	m.get = newTimeMetric(namespace, "get", reg, &errs)
	m.getSize = newSizeMetric(namespace, "get", reg, &errs)
	m.put = newTimeMetric(namespace, "put", reg, &errs)
	m.putSize = newSizeMetric(namespace, "put", reg, &errs)
	m.delete = newTimeMetric(namespace, "delete", reg, &errs)
	m.deleteSize = newSizeMetric(namespace, "delete", reg, &errs)
	m.newBatch = newTimeMetric(namespace, "new_batch", reg, &errs)
	m.newIterator = newTimeMetric(namespace, "new_iterator", reg, &errs)
	m.stat = newTimeMetric(namespace, "stat", reg, &errs)
	m.compact = newTimeMetric(namespace, "compact", reg, &errs)
	m.close = newTimeMetric(namespace, "close", reg, &errs)
	m.bPut = newTimeMetric(namespace, "batch_put", reg, &errs)
	m.bPutSize = newSizeMetric(namespace, "batch_put", reg, &errs)
	m.bDelete = newTimeMetric(namespace, "batch_delete", reg, &errs)
	m.bDeleteSize = newSizeMetric(namespace, "batch_delete", reg, &errs)
	m.bSize = newTimeMetric(namespace, "batch_size", reg, &errs)
	m.bWrite = newTimeMetric(namespace, "batch_write", reg, &errs)
	m.bWriteSize = newSizeMetric(namespace, "batch_write", reg, &errs)
	m.bReset = newTimeMetric(namespace, "batch_reset", reg, &errs)
	m.bReplay = newTimeMetric(namespace, "batch_replay", reg, &errs)
	m.bInner = newTimeMetric(namespace, "batch_inner", reg, &errs)
	m.iNext = newTimeMetric(namespace, "iterator_next", reg, &errs)
	m.iNextSize = newSizeMetric(namespace, "iterator_next", reg, &errs)
	m.iError = newTimeMetric(namespace, "iterator_error", reg, &errs)
	m.iKey = newTimeMetric(namespace, "iterator_key", reg, &errs)
	m.iValue = newTimeMetric(namespace, "iterator_value", reg, &errs)
	m.iRelease = newTimeMetric(namespace, "iterator_release", reg, &errs)
	return errs.Err
}
