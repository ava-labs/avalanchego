// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/subnet-evm/metrics"
)

type verifierStats struct {
	messageParseFail metrics.Counter
	// BlockRequest metrics
	blockSignatureValidationFail metrics.Counter
}

func newVerifierStats() *verifierStats {
	return &verifierStats{
		messageParseFail:             metrics.NewRegisteredCounter("message_parse_fail", nil),
		blockSignatureValidationFail: metrics.NewRegisteredCounter("block_signature_validation_fail", nil),
	}
}

func (h *verifierStats) IncBlockSignatureValidationFail() {
	h.blockSignatureValidationFail.Inc(1)
}

func (h *verifierStats) IncMessageParseFail() {
	h.messageParseFail.Inc(1)
}
