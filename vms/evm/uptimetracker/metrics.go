// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"time"

	"github.com/ava-labs/libevm/metrics"
)

// Metrics defines the interface for uptime tracker metrics
type Metrics interface {
	// Sync metrics
	IncSyncCount()
	UpdateSyncDuration(duration time.Duration)
	IncSyncError()

	// Validator operation metrics
	IncValidatorAdded()
	IncValidatorRemoved()
	IncValidatorStatusUpdated()
	IncValidatorOperationError()

	// Connection metrics
	IncConnectionAttempt()
	IncConnectionSuccess()
	IncDisconnectionAttempt()
	IncDisconnectionSuccess()
	IncConnectionError()
	IncDisconnectionError()

	// Pause/Resume metrics
	IncValidatorPaused()
	IncValidatorResumed()
	IncPauseError()
	IncResumeError()
}

type uptimeTrackerMetrics struct {
	// Sync metrics
	syncCount    metrics.Counter
	syncDuration metrics.Timer
	syncError    metrics.Counter

	// Validator operation metrics
	validatorAdded          metrics.Counter
	validatorRemoved        metrics.Counter
	validatorStatusUpdated  metrics.Counter
	validatorOperationError metrics.Counter

	// Connection metrics
	connectionAttempt    metrics.Counter
	connectionSuccess    metrics.Counter
	disconnectionAttempt metrics.Counter
	disconnectionSuccess metrics.Counter
	connectionError      metrics.Counter
	disconnectionError   metrics.Counter

	// Pause/Resume metrics
	validatorPaused  metrics.Counter
	validatorResumed metrics.Counter
	pauseError       metrics.Counter
	resumeError      metrics.Counter
}

// NewMetrics creates a new uptime tracker metrics instance
func NewMetrics(registry metrics.Registry) Metrics {
	return &uptimeTrackerMetrics{
		// Sync metrics
		syncCount:    metrics.GetOrRegisterCounter("uptime_tracker_sync_count", registry),
		syncDuration: metrics.GetOrRegisterTimer("uptime_tracker_sync_duration", registry),
		syncError:    metrics.GetOrRegisterCounter("uptime_tracker_sync_error", registry),

		// Validator operation metrics
		validatorAdded:          metrics.GetOrRegisterCounter("uptime_tracker_validator_added", registry),
		validatorRemoved:        metrics.GetOrRegisterCounter("uptime_tracker_validator_removed", registry),
		validatorStatusUpdated:  metrics.GetOrRegisterCounter("uptime_tracker_validator_status_updated", registry),
		validatorOperationError: metrics.GetOrRegisterCounter("uptime_tracker_validator_operation_error", registry),

		// Connection metrics
		connectionAttempt:    metrics.GetOrRegisterCounter("uptime_tracker_connection_attempt", registry),
		connectionSuccess:    metrics.GetOrRegisterCounter("uptime_tracker_connection_success", registry),
		disconnectionAttempt: metrics.GetOrRegisterCounter("uptime_tracker_disconnection_attempt", registry),
		disconnectionSuccess: metrics.GetOrRegisterCounter("uptime_tracker_disconnection_success", registry),
		connectionError:      metrics.GetOrRegisterCounter("uptime_tracker_connection_error", registry),
		disconnectionError:   metrics.GetOrRegisterCounter("uptime_tracker_disconnection_error", registry),

		// Pause/Resume metrics
		validatorPaused:  metrics.GetOrRegisterCounter("uptime_tracker_validator_paused", registry),
		validatorResumed: metrics.GetOrRegisterCounter("uptime_tracker_validator_resumed", registry),
		pauseError:       metrics.GetOrRegisterCounter("uptime_tracker_pause_error", registry),
		resumeError:      metrics.GetOrRegisterCounter("uptime_tracker_resume_error", registry),
	}
}

// Sync metrics
func (m *uptimeTrackerMetrics) IncSyncCount() {
	m.syncCount.Inc(1)
}

func (m *uptimeTrackerMetrics) UpdateSyncDuration(duration time.Duration) {
	m.syncDuration.Update(duration)
}

func (m *uptimeTrackerMetrics) IncSyncError() {
	m.syncError.Inc(1)
}

// Validator operation metrics
func (m *uptimeTrackerMetrics) IncValidatorAdded() {
	m.validatorAdded.Inc(1)
}

func (m *uptimeTrackerMetrics) IncValidatorRemoved() {
	m.validatorRemoved.Inc(1)
}

func (m *uptimeTrackerMetrics) IncValidatorStatusUpdated() {
	m.validatorStatusUpdated.Inc(1)
}

func (m *uptimeTrackerMetrics) IncValidatorOperationError() {
	m.validatorOperationError.Inc(1)
}

// Connection metrics
func (m *uptimeTrackerMetrics) IncConnectionAttempt() {
	m.connectionAttempt.Inc(1)
}

func (m *uptimeTrackerMetrics) IncConnectionSuccess() {
	m.connectionSuccess.Inc(1)
}

func (m *uptimeTrackerMetrics) IncDisconnectionAttempt() {
	m.disconnectionAttempt.Inc(1)
}

func (m *uptimeTrackerMetrics) IncDisconnectionSuccess() {
	m.disconnectionSuccess.Inc(1)
}

func (m *uptimeTrackerMetrics) IncConnectionError() {
	m.connectionError.Inc(1)
}

func (m *uptimeTrackerMetrics) IncDisconnectionError() {
	m.disconnectionError.Inc(1)
}

// Pause/Resume metrics
func (m *uptimeTrackerMetrics) IncValidatorPaused() {
	m.validatorPaused.Inc(1)
}

func (m *uptimeTrackerMetrics) IncValidatorResumed() {
	m.validatorResumed.Inc(1)
}

func (m *uptimeTrackerMetrics) IncPauseError() {
	m.pauseError.Inc(1)
}

func (m *uptimeTrackerMetrics) IncResumeError() {
	m.resumeError.Inc(1)
}

// No-op implementation for when metrics are disabled
type noopMetrics struct{}

func NewNoopMetrics() Metrics {
	return &noopMetrics{}
}

func (*noopMetrics) IncSyncCount()                    {}
func (*noopMetrics) UpdateSyncDuration(time.Duration) {}
func (*noopMetrics) IncSyncError()                    {}
func (*noopMetrics) IncValidatorAdded()               {}
func (*noopMetrics) IncValidatorRemoved()             {}
func (*noopMetrics) IncValidatorStatusUpdated()       {}
func (*noopMetrics) IncValidatorOperationError()      {}
func (*noopMetrics) IncConnectionAttempt()            {}
func (*noopMetrics) IncConnectionSuccess()            {}
func (*noopMetrics) IncDisconnectionAttempt()         {}
func (*noopMetrics) IncDisconnectionSuccess()         {}
func (*noopMetrics) IncConnectionError()              {}
func (*noopMetrics) IncDisconnectionError()           {}
func (*noopMetrics) IncValidatorPaused()              {}
func (*noopMetrics) IncValidatorResumed()             {}
func (*noopMetrics) IncPauseError()                   {}
func (*noopMetrics) IncResumeError()                  {}
