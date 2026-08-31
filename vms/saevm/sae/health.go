// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/snow"
)

// Consensus states reported by [Health.State].
const (
	HealthStateInitializing  = "initializing"
	HealthStateStateSyncing  = "stateSyncing"
	HealthStateBootstrapping = "bootstrapping"
	HealthStateNormalOp      = "normalOp"
	HealthStateUnknown       = "unknown"
)

// State sync outcomes reported by [StateSync.Status]. Only [StateSyncSyncing]
// and [StateSyncCompleted] describe a chain whose state was synced; every other
// status means the chain is bootstrapping, or has bootstrapped, from genesis.
const (
	// StateSyncDisabled means state sync is configured off, so the VM will not
	// ask the engine for summaries.
	StateSyncDisabled = "disabled"
	// StateSyncNotStarted means state sync is enabled but the engine has not
	// offered a summary yet.
	StateSyncNotStarted = "notStarted"
	// StateSyncSkipped means a summary was offered but the VM declined to sync
	// it, e.g. because the chain has already accepted blocks.
	StateSyncSkipped = "skipped"
	// StateSyncSyncing means a summary was accepted and its sync is running.
	StateSyncSyncing = "syncing"
	// StateSyncCompleted means the accepted summary's sync finished
	// successfully.
	StateSyncCompleted = "completed"
	// StateSyncFailed means the accepted summary's sync terminated with an
	// error, which is fatal to the chain.
	StateSyncFailed = "failed"
)

// Health is the detail returned by [VM.HealthCheck]. It is serialized into the
// node's health API response under the chain's `message.engine.vm` key, so the
// JSON field names below are an observable interface: renaming one is a
// breaking change for operators and tooling.
type Health struct {
	// State is the consensus state the VM is currently in.
	State string `json:"state"`
	// StateScheme is the resolved trie database scheme the VM is running with.
	StateScheme string `json:"stateScheme"`
	// StateSync reports the outcome of state sync. It is omitted by VMs that do
	// not implement state sync; [VM] itself never populates it, because the
	// summary handler that performs a sync lives above [VM].
	StateSync *StateSync `json:"stateSync,omitempty"`
}

// StateSync is the state sync detail reported under [Health.StateSync]. Build
// one with [StateSyncProgress.Details] rather than by hand, so that Status stays
// consistent with the other fields.
type StateSync struct {
	// Status is the outcome of state sync.
	Status string `json:"status"`
	// SummaryHeight and SummaryHash identify the state summary being synced, and
	// so which state the chain below that height was synced to rather than
	// executed. Both are omitted unless a sync was started.
	SummaryHeight uint64 `json:"summaryHeight,omitempty"`
	SummaryHash   string `json:"summaryHash,omitempty"`
	// Error is the message of the error that terminated a failed sync. It is
	// omitted unless Status is [StateSyncFailed].
	Error string `json:"error,omitempty"`
}

// StateSyncProgress is the observable lifecycle of a state sync. It is declared
// here, alongside [Health], so that the reported wire format and the rules for
// deriving it live in one place; the summary handlers that own a sync's
// lifecycle populate it.
type StateSyncProgress struct {
	// Enabled reports whether state sync is configured on.
	Enabled bool
	// Skipped reports whether an offered summary was declined. It is mutually
	// exclusive with Started, because a summary is only synced once accepted.
	Skipped bool
	// Started reports whether an accepted summary's sync was launched. It MUST
	// be false until the summary is known.
	Started bool
	// SummaryHeight and SummaryHash are the accepted summary's block. They are
	// only read when Started is true.
	SummaryHeight uint64
	SummaryHash   common.Hash
	// Finished reports whether the sync has terminated, in success or failure.
	// It MUST only be true once Err is safe to read.
	Finished bool
	// Err is the error that terminated the sync, if any. It is only read when
	// Finished is true.
	Err error
}

// Details converts p into the [StateSync] health detail served by the node's
// health API.
func (p StateSyncProgress) Details() *StateSync {
	d := new(StateSync)

	switch {
	case !p.Started:
		// A sync that never started MUST NOT be reported as running, even when
		// it is enabled: the engine may not have offered a summary yet, or the
		// VM may have declined the one it offered.
		switch {
		case p.Skipped:
			d.Status = StateSyncSkipped
		case p.Enabled:
			d.Status = StateSyncNotStarted
		default:
			d.Status = StateSyncDisabled
		}
		return d

	case !p.Finished:
		d.Status = StateSyncSyncing

	case p.Err != nil:
		d.Status = StateSyncFailed
		d.Error = p.Err.Error()

	default:
		d.Status = StateSyncCompleted
	}

	d.SummaryHeight = p.SummaryHeight
	d.SummaryHash = p.SummaryHash.Hex()
	return d
}

// HealthCheck reports the VM's consensus state and trie database scheme.
//
// It never reports the chain as unhealthy: these details are informational, and
// the conditions that make a chain unhealthy are reported by the engine and by
// the node's own health checks.
func (vm *VM) HealthCheck(context.Context) (any, error) {
	return Health{
		State:       HealthState(vm.consensusState.Get()),
		StateScheme: vm.config.DBConfig.ResolvedScheme(),
	}, nil
}

// HealthState maps a consensus state to its health representation. The
// [snow.State] stringer is not used because its output is prose rather than a
// stable identifier.
func HealthState(state snow.State) string {
	switch state {
	case snow.Initializing:
		return HealthStateInitializing
	case snow.StateSyncing:
		return HealthStateStateSyncing
	case snow.Bootstrapping:
		return HealthStateBootstrapping
	case snow.NormalOp:
		return HealthStateNormalOp
	default:
		return HealthStateUnknown
	}
}
