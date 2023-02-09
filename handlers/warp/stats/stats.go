// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"sync"
	"time"

	"github.com/ava-labs/subnet-evm/metrics"
)

var (
	_ SignatureRequestHandlerStats = (*handlerStats)(nil)
	_ SignatureRequestHandlerStats = (*MockSignatureRequestHandlerStats)(nil)
)

type SignatureRequestHandlerStats interface {
	IncSignatureRequest()
	IncSignatureHit()
	IncSignatureMiss()
	UpdateSignatureRequestTime(duration time.Duration)
}

type handlerStats struct {
	// SignatureRequestHandler metrics
	signatureRequest        metrics.Counter
	signatureHit            metrics.Counter
	signatureMiss           metrics.Counter
	signatureProcessingTime metrics.Timer
}

func NewStats(enabled bool) SignatureRequestHandlerStats {
	if !enabled {
		return &MockSignatureRequestHandlerStats{}
	}

	return &handlerStats{
		signatureRequest:        metrics.GetOrRegisterCounter("signature_request_count", nil),
		signatureHit:            metrics.GetOrRegisterCounter("signature_request_hit", nil),
		signatureMiss:           metrics.GetOrRegisterCounter("signature_request_miss", nil),
		signatureProcessingTime: metrics.GetOrRegisterTimer("signature_request_duration", nil),
	}
}

func (h *handlerStats) IncSignatureRequest() { h.signatureRequest.Inc(1) }
func (h *handlerStats) IncSignatureHit()     { h.signatureHit.Inc(1) }
func (h *handlerStats) IncSignatureMiss()    { h.signatureMiss.Inc(1) }
func (h *handlerStats) UpdateSignatureRequestTime(duration time.Duration) {
	h.signatureProcessingTime.Update(duration)
}

// MockSignatureRequestHandlerStats is mock for capturing and asserting on handler metrics in test
type MockSignatureRequestHandlerStats struct {
	lock sync.Mutex

	SignatureRequestCount,
	SignatureRequestHit,
	SignatureRequestMiss uint32
	SignatureRequestDuration time.Duration
}

func (m *MockSignatureRequestHandlerStats) Reset() {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.SignatureRequestCount = 0
	m.SignatureRequestHit = 0
	m.SignatureRequestMiss = 0
	m.SignatureRequestDuration = 0
}

func (m *MockSignatureRequestHandlerStats) IncSignatureRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SignatureRequestCount++
}

func (m *MockSignatureRequestHandlerStats) IncSignatureHit() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SignatureRequestHit++
}

func (m *MockSignatureRequestHandlerStats) IncSignatureMiss() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SignatureRequestMiss++
}

func (m *MockSignatureRequestHandlerStats) UpdateSignatureRequestTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SignatureRequestDuration += duration
}
