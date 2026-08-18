// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "github.com/ava-labs/avalanchego/vms/saevm/sae"

// SyncProgress reports the observable lifecycle of the sync launched by
// [SummaryHandler.AcceptSummary], to be served as part of [sae.Health].
//
// It overrides the embedded handler's method because this handler extends the
// sync with the C-Chain atomic trie phase and therefore owns the lifecycle that
// determines completion; the embedded handler's own lifecycle is never reached.
//
// SyncProgress never blocks, so it is safe to call from a health check while the
// chain lock is held.
func (h *SummaryHandler) SyncProgress() sae.StateSyncProgress {
	var finished bool
	select {
	case <-h.done:
		finished = true
	default:
	}
	return h.SummaryHandler.SyncProgressOf(finished, h.err.Get())
}

// Health reports the state sync detail served under [sae.Health.StateSync].
func (h *SummaryHandler) Health() *sae.StateSync {
	return h.SyncProgress().Details()
}
