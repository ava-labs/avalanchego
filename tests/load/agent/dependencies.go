// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package agent

import (
	"context"

	"github.com/ava-labs/avalanchego/tests"
)

type TxGenerator[T comparable] interface {
	// GenerateTx returns a valid transaction.
	GenerateTx() (T, error)
}

type Issuer[T comparable] interface {
	// Listen for the final status of transactions and notify the tracker
	// Listen stops if the context is done, an error occurs, or if the issuer
	// has sent all their transactions.
	// Listen MUST return a nil error if the context is canceled.
	Listen(tc tests.TestContext, ctx context.Context) error

	// Stop notifies the issuer that no further transactions will be issued.
	// If a transaction is issued after Stop has been called, the issuer should error.
	Stop()

	// Issue sends a tx to the network, and informs the tracker that its sent
	// said transaction.
	IssueTx(tc tests.TestContext, ctx context.Context, tx T) error
}

// Tracker keeps track of the status of transactions.
// This must be thread-safe, so it can be called in parallel by the issuer(s) or orchestrator.
type Tracker[T comparable] interface {
	// IssueStart records a transaction that is being issued.
	IssueStart(T)
	// IssueEnd records a transaction that was issued, but whose final status is
	// not yet known.
	IssueEnd(T)
	// ObserveConfirmed records a transaction that was confirmed.
	ObserveConfirmed(T)
	// ObserveFailed records a transaction that failed (e.g. expired)
	ObserveFailed(T)

	// GetObservedConfirmed returns the number of transactions that the tracker has
	// confirmed were accepted.
	GetObservedConfirmed() uint64
	// GetObservedFailed returns the number of transactions that the tracker has
	// confirmed failed.
	GetObservedFailed() uint64
}
