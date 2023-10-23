// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"time"

	"github.com/ava-labs/subnet-evm/metrics"
)

type handlerStats struct {
	// MessageSignatureRequestHandler metrics
	messageSignatureRequest         metrics.Counter
	messageSignatureHit             metrics.Counter
	messageSignatureMiss            metrics.Counter
	messageSignatureRequestDuration metrics.Gauge
	// BlockSignatureRequestHandler metrics
	blockSignatureRequest         metrics.Counter
	blockSignatureHit             metrics.Counter
	blockSignatureMiss            metrics.Counter
	blockSignatureRequestDuration metrics.Gauge
}

func newStats() *handlerStats {
	return &handlerStats{
		messageSignatureRequest:         metrics.GetOrRegisterCounter("message_signature_request_count", nil),
		messageSignatureHit:             metrics.GetOrRegisterCounter("message_signature_request_hit", nil),
		messageSignatureMiss:            metrics.GetOrRegisterCounter("message_signature_request_miss", nil),
		messageSignatureRequestDuration: metrics.GetOrRegisterGauge("message_signature_request_duration", nil),
		blockSignatureRequest:           metrics.GetOrRegisterCounter("block_signature_request_count", nil),
		blockSignatureHit:               metrics.GetOrRegisterCounter("block_signature_request_hit", nil),
		blockSignatureMiss:              metrics.GetOrRegisterCounter("block_signature_request_miss", nil),
		blockSignatureRequestDuration:   metrics.GetOrRegisterGauge("block_signature_request_duration", nil),
	}
}

func (h *handlerStats) IncMessageSignatureRequest() { h.messageSignatureRequest.Inc(1) }
func (h *handlerStats) IncMessageSignatureHit()     { h.messageSignatureHit.Inc(1) }
func (h *handlerStats) IncMessageSignatureMiss()    { h.messageSignatureMiss.Inc(1) }
func (h *handlerStats) UpdateMessageSignatureRequestTime(duration time.Duration) {
	h.messageSignatureRequestDuration.Inc(int64(duration))
}
func (h *handlerStats) IncBlockSignatureRequest() { h.blockSignatureRequest.Inc(1) }
func (h *handlerStats) IncBlockSignatureHit()     { h.blockSignatureHit.Inc(1) }
func (h *handlerStats) IncBlockSignatureMiss()    { h.blockSignatureMiss.Inc(1) }
func (h *handlerStats) UpdateBlockSignatureRequestTime(duration time.Duration) {
	h.blockSignatureRequestDuration.Inc(int64(duration))
}
func (h *handlerStats) Clear() {
	h.messageSignatureRequest.Clear()
	h.messageSignatureHit.Clear()
	h.messageSignatureMiss.Clear()
	h.messageSignatureRequestDuration.Update(0)
	h.blockSignatureRequest.Clear()
	h.blockSignatureHit.Clear()
	h.blockSignatureMiss.Clear()
	h.blockSignatureRequestDuration.Update(0)
}
