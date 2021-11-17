// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type blockMetrics struct {
	buildBlock,
	buildBlockErr,
	parseBlock,
	parseBlockErr,
	getBlock,
	getBlockErr,
	setPreference,
	lastAccepted,
	verify,
	verifyErr,
	accept,
	reject,
	getAncestors,
	batchedParseBlock metric.Averager
}

func (m *blockMetrics) Initialize(
	supportsBatchedFetching bool,
	namespace string,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.buildBlock = newAverager(namespace, "build_block", reg, &errs)
	m.buildBlockErr = newAverager(namespace, "build_block_err", reg, &errs)
	m.parseBlock = newAverager(namespace, "parse_block", reg, &errs)
	m.parseBlockErr = newAverager(namespace, "parse_block_err", reg, &errs)
	m.getBlock = newAverager(namespace, "get_block", reg, &errs)
	m.getBlockErr = newAverager(namespace, "get_block_err", reg, &errs)
	m.setPreference = newAverager(namespace, "set_preference", reg, &errs)
	m.lastAccepted = newAverager(namespace, "last_accepted", reg, &errs)
	m.verify = newAverager(namespace, "verify", reg, &errs)
	m.verifyErr = newAverager(namespace, "verify_err", reg, &errs)
	m.accept = newAverager(namespace, "accept", reg, &errs)
	m.reject = newAverager(namespace, "reject", reg, &errs)

	if supportsBatchedFetching {
		m.getAncestors = newAverager(namespace, "get_ancestors", reg, &errs)
		m.batchedParseBlock = newAverager(namespace, "batched_parse_block", reg, &errs)
	}
	return errs.Err
}
