// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"time"

	"github.com/ava-labs/subnet-evm/metrics"
)

type handlerStats struct {
	// SignatureRequestHandler metrics
	signatureRequest         metrics.Counter
	signatureHit             metrics.Counter
	signatureMiss            metrics.Counter
	signatureRequestDuration metrics.Gauge
}

func newStats() *handlerStats {
	return &handlerStats{
		signatureRequest:         metrics.GetOrRegisterCounter("signature_request_count", nil),
		signatureHit:             metrics.GetOrRegisterCounter("signature_request_hit", nil),
		signatureMiss:            metrics.GetOrRegisterCounter("signature_request_miss", nil),
		signatureRequestDuration: metrics.GetOrRegisterGauge("signature_request_duration", nil),
	}
}

func (h *handlerStats) IncSignatureRequest() { h.signatureRequest.Inc(1) }
func (h *handlerStats) IncSignatureHit()     { h.signatureHit.Inc(1) }
func (h *handlerStats) IncSignatureMiss()    { h.signatureMiss.Inc(1) }
func (h *handlerStats) UpdateSignatureRequestTime(duration time.Duration) {
	h.signatureRequestDuration.Inc(int64(duration))
}
func (h *handlerStats) Clear() {
	h.signatureRequest.Clear()
	h.signatureHit.Clear()
	h.signatureMiss.Clear()
	h.signatureRequestDuration.Update(0)
}
