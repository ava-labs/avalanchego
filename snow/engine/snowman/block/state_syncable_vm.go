// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

type StateSyncableVM interface {
	// Enabled indicates whether the state sync is enabled for this VM
	Enabled() bool
	// StateSummary returns latest StateSummary with an optional error
	StateSummary() ([]byte, error)
	// IsAccepted returns true if input []bytes represent a valid state summary
	// for fast sync.
	IsAccepted([]byte) (bool, error)

	// Sync state is with a list of valid state summaries to sync from.
	// These summaries were collected from peers and validated with validators.
	// VM will use information inside the summary to choose one and sync
	// its state to that summary. Normal bootstrapping resumes after this
	// function returns.
	// Will be called with [nil] if no valid state summaries could be found.
	SyncState([][]byte) error
}
