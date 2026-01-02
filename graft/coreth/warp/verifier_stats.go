// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import "github.com/ava-labs/libevm/metrics"

type verifierStats struct {
	messageParseFail metrics.Counter
	// BlockRequest metrics
	blockValidationFail metrics.Counter
}

func newVerifierStats() *verifierStats {
	return &verifierStats{
		messageParseFail:    metrics.NewRegisteredCounter("warp_backend_message_parse_fail", nil),
		blockValidationFail: metrics.NewRegisteredCounter("warp_backend_block_validation_fail", nil),
	}
}

func (h *verifierStats) IncBlockValidationFail() {
	h.blockValidationFail.Inc(1)
}

func (h *verifierStats) IncMessageParseFail() {
	h.messageParseFail.Inc(1)
}
