// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
)

var (
	_ ClientSyncerStats = (*clientSyncerStats)(nil)
	_ ClientSyncerStats = (*noopStats)(nil)
)

type ClientSyncerStats interface {
	GetMetric(message.Request) (MessageMetric, error)
}

type MessageMetric interface {
	IncRequested()
	IncSucceeded()
	IncFailed()
	IncInvalidResponse()
	IncReceived(int64)
	UpdateRequestLatency(time.Duration)
}

type messageMetric struct {
	requested       metrics.Counter // Number of times a request has been sent
	succeeded       metrics.Counter // Number of times a request has succeeded
	failed          metrics.Counter // Number of times a request failed (does not include invalid responses)
	invalidResponse metrics.Counter // Number of times a request failed due to an invalid response
	received        metrics.Counter // Number of items that have been received

	requestLatency metrics.Timer // Latency for this request
}

func NewMessageMetric(name string) MessageMetric {
	return &messageMetric{
		requested:       metrics.GetOrRegisterCounter(fmt.Sprintf("%s_requested", name), nil),
		succeeded:       metrics.GetOrRegisterCounter(fmt.Sprintf("%s_succeeded", name), nil),
		failed:          metrics.GetOrRegisterCounter(fmt.Sprintf("%s_failed", name), nil),
		invalidResponse: metrics.GetOrRegisterCounter(fmt.Sprintf("%s_invalid_response", name), nil),
		received:        metrics.GetOrRegisterCounter(fmt.Sprintf("%s_received", name), nil),
		requestLatency:  metrics.GetOrRegisterTimer(fmt.Sprintf("%s_request_latency", name), nil),
	}
}

func (m *messageMetric) IncRequested() {
	m.requested.Inc(1)
}

func (m *messageMetric) IncSucceeded() {
	m.succeeded.Inc(1)
}

func (m *messageMetric) IncFailed() {
	m.failed.Inc(1)
}

func (m *messageMetric) IncInvalidResponse() {
	m.invalidResponse.Inc(1)
}

func (m *messageMetric) IncReceived(size int64) {
	m.received.Inc(size)
}

func (m *messageMetric) UpdateRequestLatency(duration time.Duration) {
	m.requestLatency.Update(duration)
}

type clientSyncerStats struct {
	stateTrieLeavesMetric,
	codeRequestMetric,
	blockRequestMetric MessageMetric
}

// NewClientSyncerStats returns stats for the client syncer
func NewClientSyncerStats() ClientSyncerStats {
	return &clientSyncerStats{
		stateTrieLeavesMetric: NewMessageMetric("sync_state_trie_leaves"),
		codeRequestMetric:     NewMessageMetric("sync_code"),
		blockRequestMetric:    NewMessageMetric("sync_blocks"),
	}
}

// GetMetric returns the appropriate messaage metric for the given request
func (c *clientSyncerStats) GetMetric(msgIntf message.Request) (MessageMetric, error) {
	switch msg := msgIntf.(type) {
	case message.BlockRequest:
		return c.blockRequestMetric, nil
	case message.CodeRequest:
		return c.codeRequestMetric, nil
	case message.LeafsRequest:
		return c.stateTrieLeavesMetric, nil
	default:
		return nil, fmt.Errorf("attempted to get metric for invalid request with type %T", msg)
	}
}

// no-op implementation of ClientSyncerStats
type noopStats struct {
	noop noopMsgMetric
}

type noopMsgMetric struct{}

func (noopMsgMetric) IncRequested()                      {}
func (noopMsgMetric) IncSucceeded()                      {}
func (noopMsgMetric) IncFailed()                         {}
func (noopMsgMetric) IncInvalidResponse()                {}
func (noopMsgMetric) IncReceived(int64)                  {}
func (noopMsgMetric) UpdateRequestLatency(time.Duration) {}

func NewNoOpStats() ClientSyncerStats {
	return &noopStats{}
}

func (n noopStats) GetMetric(_ message.Request) (MessageMetric, error) {
	return n.noop, nil
}

// NewStats returns syncer stats if enabled or a no-op version if disabled.
func NewStats(enabled bool) ClientSyncerStats {
	if enabled {
		return NewClientSyncerStats()
	} else {
		return NewNoOpStats()
	}
}
