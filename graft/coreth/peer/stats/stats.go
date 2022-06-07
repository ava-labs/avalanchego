// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"time"

	"github.com/ava-labs/coreth/metrics"
)

// RequestHandlerStats provides the interface for metrics on request handling.
// Since we drop
type RequestHandlerStats interface {
	UpdateTimeUntilDeadline(duration time.Duration)
	IncDeadlineDroppedRequest()
}

type requestHandlerStats struct {
	timeUntilDeadline metrics.Timer
	droppedRequests   metrics.Counter
}

func (h *requestHandlerStats) IncDeadlineDroppedRequest() {
	h.droppedRequests.Inc(1)
}

func (h *requestHandlerStats) UpdateTimeUntilDeadline(duration time.Duration) {
	h.timeUntilDeadline.Update(duration)
}

func NewRequestHandlerStats() RequestHandlerStats {
	return &requestHandlerStats{
		timeUntilDeadline: metrics.GetOrRegisterTimer("net_req_time_until_deadline", nil),
		droppedRequests:   metrics.GetOrRegisterCounter("net_req_deadline_dropped", nil),
	}
}
