// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import "github.com/ava-labs/libevm/metrics"

type verifierStats struct {
	messageParseFail metrics.Counter
	// AddressedCall metrics
	addressedCallValidationFail metrics.Counter
	// BlockRequest metrics
	blockValidationFail metrics.Counter
	// Uptime metrics
	uptimeValidationFail metrics.Counter
}

func newVerifierStats() *verifierStats {
	return &verifierStats{
		messageParseFail:            metrics.NewRegisteredCounter("warp_backend_message_parse_fail", nil),
		addressedCallValidationFail: metrics.NewRegisteredCounter("warp_backend_addressed_call_validation_fail", nil),
		blockValidationFail:         metrics.NewRegisteredCounter("warp_backend_block_validation_fail", nil),
		uptimeValidationFail:        metrics.NewRegisteredCounter("warp_backend_uptime_validation_fail", nil),
	}
}

func (h *verifierStats) IncAddressedCallValidationFail() {
	h.addressedCallValidationFail.Inc(1)
}

func (h *verifierStats) IncBlockValidationFail() {
	h.blockValidationFail.Inc(1)
}

func (h *verifierStats) IncMessageParseFail() {
	h.messageParseFail.Inc(1)
}

func (h *verifierStats) IncUptimeValidationFail() {
	h.uptimeValidationFail.Inc(1)
}
