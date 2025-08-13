// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type Metrics interface {
	metric.APIInterceptor
}

type metrics struct {
	metric.APIInterceptor
}

func New(registerer prometheus.Registerer) (Metrics, error) {
	m := &metrics{}
	apiRequestMetrics, err := metric.NewAPIInterceptor(registerer)
	errs := wrappers.Errs{Err: err}
	m.APIInterceptor = apiRequestMetrics

	return m, errs.Err
}
