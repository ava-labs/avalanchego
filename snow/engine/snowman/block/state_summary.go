// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// StateSummary represents all the information needed to download, verify, and
// rebuild its state.
type StateSummary interface {
	// ID uniquely identifies this state summary, regardless of the chain state.
	ID() ids.ID

	// Height uniquely identifies this an accepted state summary.
	Height() uint64

	// Bytes returns a byte slice than can be used to reconstruct this summary.
	Bytes() []byte

	// Accept notifies the VM that this is a canonically accepted state summary.
	// The VM is expected to return how it plans on handling this summary via
	// the [StateSyncMode].
	//
	// Invariant: The VM must be able to correctly handle the acceptance of a
	// state summary that is older than its current state.
	//
	// See [StateSyncSkipped], [StateSyncStatic], and [StateSyncDynamic] for the
	// expected invariants the VM must maintain based on the return value.
	Accept(context.Context) (StateSyncMode, error)
}
