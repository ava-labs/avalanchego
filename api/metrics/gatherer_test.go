// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var counterOpts = prometheus.CounterOpts{
	Name: "counter",
	Help: "help",
}

type testGatherer struct {
	mfs []*dto.MetricFamily
	err error
}

func (g *testGatherer) Gather() ([]*dto.MetricFamily, error) {
	return g.mfs, g.err
}
