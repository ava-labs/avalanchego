// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"time"

	"github.com/ethereum/go-ethereum/metrics"
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
		messageSignatureRequest:         metrics.NewRegisteredCounter("message_signature_request_count", nil),
		messageSignatureHit:             metrics.NewRegisteredCounter("message_signature_request_hit", nil),
		messageSignatureMiss:            metrics.NewRegisteredCounter("message_signature_request_miss", nil),
		messageSignatureRequestDuration: metrics.NewRegisteredGauge("message_signature_request_duration", nil),
		blockSignatureRequest:           metrics.NewRegisteredCounter("block_signature_request_count", nil),
		blockSignatureHit:               metrics.NewRegisteredCounter("block_signature_request_hit", nil),
		blockSignatureMiss:              metrics.NewRegisteredCounter("block_signature_request_miss", nil),
		blockSignatureRequestDuration:   metrics.NewRegisteredGauge("block_signature_request_duration", nil),
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
