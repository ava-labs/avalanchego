// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "github.com/ava-labs/avalanchego/vms/saevm/sae"

// SyncProgress reports the observable lifecycle of the sync launched by
// [SummaryHandler.AcceptSummary], to be served as part of [sae.Health].
//
// SyncProgress never blocks, so it is safe to call from a health check while the
// chain lock is held.
func (h *SummaryHandler) SyncProgress() sae.StateSyncProgress {
	return h.SyncProgressOf(isClosed(h.done), h.err.Get())
}

// SyncProgressOf is [SummaryHandler.SyncProgress] with the sync's completion
// supplied by the caller. It exists for handlers that wrap this one and extend
// the sync with further phases, and therefore own the lifecycle that determines
// completion: such a handler MUST report its own completion, because this
// handler's is only reached by [SummaryHandler.AcceptSummary]. All other callers
// SHOULD use [SummaryHandler.SyncProgress].
//
// err is only reported once finished is true, honoring the rule that a
// handler's error is written before its done channel is closed and read only
// after.
func (h *SummaryHandler) SyncProgressOf(finished bool, err error) sae.StateSyncProgress {
	p := sae.StateSyncProgress{
		Enabled: h.cfg.Enabled,
		Skipped: h.skipped.Get(),
	}

	// [SummaryHandler.StateSync] records the target, so it is set for a sync
	// started either by this handler or by one wrapping it.
	target := h.target.Get()
	if target == nil {
		// No sync was started, so there is nothing to report on.
		return p
	}

	p.Started = true
	p.SummaryHeight = target.AcceptedHeight
	p.SummaryHash = target.AcceptedHash
	p.Finished = finished
	if finished {
		p.Err = err
	}
	return p
}

// Health reports the state sync detail served under [sae.Health.StateSync].
func (h *SummaryHandler) Health() *sae.StateSync {
	return h.SyncProgress().Details()
}

// isClosed reports whether done has been closed, without blocking.
func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
