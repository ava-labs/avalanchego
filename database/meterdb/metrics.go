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
	has, hasSize,
	get, getSize,
	put, putSize,
	delete, deleteSize,
	newBatch,
	newIterator,
	compact,
	close,
	healthCheck,
	bPut, bPutSize,
	bDelete, bDeleteSize,
	bSize,
	bWrite, bWriteSize,
	bReset,
	bReplay,
	bInner,
	iNext, iNextSize,
	iError,
	iKey,
	iValue,
	iRelease metric.Averager
}

func newMetrics(namespace string, reg prometheus.Registerer) (metrics, error) {
	errs := wrappers.Errs{}
	return metrics{
		readSize:    newSizeMetric(namespace, "read", reg, &errs),
		writeSize:   newSizeMetric(namespace, "write", reg, &errs),
		has:         newTimeMetric(namespace, "has", reg, &errs),
		hasSize:     newSizeMetric(namespace, "has", reg, &errs),
		get:         newTimeMetric(namespace, "get", reg, &errs),
		getSize:     newSizeMetric(namespace, "get", reg, &errs),
		put:         newTimeMetric(namespace, "put", reg, &errs),
		putSize:     newSizeMetric(namespace, "put", reg, &errs),
		delete:      newTimeMetric(namespace, "delete", reg, &errs),
		deleteSize:  newSizeMetric(namespace, "delete", reg, &errs),
		newBatch:    newTimeMetric(namespace, "new_batch", reg, &errs),
		newIterator: newTimeMetric(namespace, "new_iterator", reg, &errs),
		compact:     newTimeMetric(namespace, "compact", reg, &errs),
		close:       newTimeMetric(namespace, "close", reg, &errs),
		healthCheck: newTimeMetric(namespace, "health_check", reg, &errs),
		bPut:        newTimeMetric(namespace, "batch_put", reg, &errs),
		bPutSize:    newSizeMetric(namespace, "batch_put", reg, &errs),
		bDelete:     newTimeMetric(namespace, "batch_delete", reg, &errs),
		bDeleteSize: newSizeMetric(namespace, "batch_delete", reg, &errs),
		bSize:       newTimeMetric(namespace, "batch_size", reg, &errs),
		bWrite:      newTimeMetric(namespace, "batch_write", reg, &errs),
		bWriteSize:  newSizeMetric(namespace, "batch_write", reg, &errs),
		bReset:      newTimeMetric(namespace, "batch_reset", reg, &errs),
		bReplay:     newTimeMetric(namespace, "batch_replay", reg, &errs),
		bInner:      newTimeMetric(namespace, "batch_inner", reg, &errs),
		iNext:       newTimeMetric(namespace, "iterator_next", reg, &errs),
		iNextSize:   newSizeMetric(namespace, "iterator_next", reg, &errs),
		iError:      newTimeMetric(namespace, "iterator_error", reg, &errs),
		iKey:        newTimeMetric(namespace, "iterator_key", reg, &errs),
		iValue:      newTimeMetric(namespace, "iterator_value", reg, &errs),
		iRelease:    newTimeMetric(namespace, "iterator_release", reg, &errs),
	}, errs.Err
}
