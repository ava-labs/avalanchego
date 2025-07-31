// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	// Height metrics
	getBlockIDAtHeight,
	// Block verification with context metrics
	shouldVerifyWithContext,
	verifyWithContext,
	verifyWithContextErr,
	// Block building with context metrics
	buildBlockWithContext,
	buildBlockWithContextErr,
	// Batched metrics
	getAncestors,
	batchedParseBlock,
	// State sync metrics
	stateSyncEnabled,
	getOngoingSyncStateSummary,
	getLastStateSummary,
	parseStateSummary,
	parseStateSummaryErr,
	getStateSummary,
	getStateSummaryErr metric.Averager
}

func (m *blockMetrics) Initialize(
	supportsBlockBuildingWithContext bool,
	supportsBatchedFetching bool,
	supportsStateSync bool,
	reg prometheus.Registerer,
) error {
	errs := wrappers.Errs{}
	m.buildBlock = newAverager("build_block", reg, &errs)
	m.buildBlockErr = newAverager("build_block_err", reg, &errs)
	m.parseBlock = newAverager("parse_block", reg, &errs)
	m.parseBlockErr = newAverager("parse_block_err", reg, &errs)
	m.getBlock = newAverager("get_block", reg, &errs)
	m.getBlockErr = newAverager("get_block_err", reg, &errs)
	m.setPreference = newAverager("set_preference", reg, &errs)
	m.lastAccepted = newAverager("last_accepted", reg, &errs)
	m.verify = newAverager("verify", reg, &errs)
	m.verifyErr = newAverager("verify_err", reg, &errs)
	m.accept = newAverager("accept", reg, &errs)
	m.reject = newAverager("reject", reg, &errs)
	m.shouldVerifyWithContext = newAverager("should_verify_with_context", reg, &errs)
	m.verifyWithContext = newAverager("verify_with_context", reg, &errs)
	m.verifyWithContextErr = newAverager("verify_with_context_err", reg, &errs)
	m.getBlockIDAtHeight = newAverager("get_block_id_at_height", reg, &errs)

	if supportsBlockBuildingWithContext {
		m.buildBlockWithContext = newAverager("build_block_with_context", reg, &errs)
		m.buildBlockWithContextErr = newAverager("build_block_with_context_err", reg, &errs)
	}
	if supportsBatchedFetching {
		m.getAncestors = newAverager("get_ancestors", reg, &errs)
		m.batchedParseBlock = newAverager("batched_parse_block", reg, &errs)
	}
	if supportsStateSync {
		m.stateSyncEnabled = newAverager("state_sync_enabled", reg, &errs)
		m.getOngoingSyncStateSummary = newAverager("get_ongoing_state_sync_summary", reg, &errs)
		m.getLastStateSummary = newAverager("get_last_state_summary", reg, &errs)
		m.parseStateSummary = newAverager("parse_state_summary", reg, &errs)
		m.parseStateSummaryErr = newAverager("parse_state_summary_err", reg, &errs)
		m.getStateSummary = newAverager("get_state_summary", reg, &errs)
		m.getStateSummaryErr = newAverager("get_state_summary_err", reg, &errs)
	}
	return errs.Err
}
