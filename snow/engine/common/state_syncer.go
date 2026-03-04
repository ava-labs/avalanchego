// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "context"

// StateSyncer controls the selection and verification of state summaries
// to drive VM state syncing. It collects the latest state summaries and elicit
// votes on them, making sure that a qualified majority of nodes support the
// selected state summary.
type StateSyncer interface {
	Engine

	// IsEnabled returns true if the underlying VM wants to perform state sync.
	// Any returned error will be considered fatal.
	IsEnabled(context.Context) (bool, error)
}
