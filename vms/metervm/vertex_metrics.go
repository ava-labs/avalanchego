// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type vertexMetrics struct {
	parse,
	parseErr,
	verify,
	verifyErr,
	accept,
	reject metric.Averager
}

func (m *vertexMetrics) Initialize(reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.parse = newAverager("parse_tx", reg, &errs)
	m.parseErr = newAverager("parse_tx_err", reg, &errs)
	m.verify = newAverager("verify_tx", reg, &errs)
	m.verifyErr = newAverager("verify_tx_err", reg, &errs)
	m.accept = newAverager("accept", reg, &errs)
	m.reject = newAverager("reject", reg, &errs)
	return errs.Err
}
