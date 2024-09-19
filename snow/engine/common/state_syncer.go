// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/api/health"
)

// StateSyncer controls the selection and verification of state summaries
// to drive VM state syncing. It collects the latest state summaries and elicit
// votes on them, making sure that a qualified majority of nodes support the
// selected state summary.
type StateSyncer interface {
	StateSummaryFrontierHandler

	AcceptedStateSummaryHandler

	InternalHandler

	// Start engine operations from given request ID
	Start(ctx context.Context, startReqID uint32) error

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checker

	// IsEnabled returns true if the underlying VM wants to perform state sync.
	// Any returned error will be considered fatal.
	IsEnabled(context.Context) (bool, error)
}
