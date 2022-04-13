// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/utils/metric"
	"github.com/chain4travel/caminogo/utils/wrappers"
)

type vertexMetrics struct {
	pending,
	parse,
	parseErr,
	get,
	getErr,
	verify,
	verifyErr,
	accept,
	reject metric.Averager
}

func (m *vertexMetrics) Initialize(
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.pending = newAverager(namespace, "pending_txs", reg, &errs)
	m.parse = newAverager(namespace, "parse_tx", reg, &errs)
	m.parseErr = newAverager(namespace, "parse_tx_err", reg, &errs)
	m.get = newAverager(namespace, "get_tx", reg, &errs)
	m.getErr = newAverager(namespace, "get_tx_err", reg, &errs)
	m.verify = newAverager(namespace, "verify_tx", reg, &errs)
	m.verifyErr = newAverager(namespace, "verify_tx_err", reg, &errs)
	m.accept = newAverager(namespace, "accept", reg, &errs)
	m.reject = newAverager(namespace, "reject", reg, &errs)
	return errs.Err
}
