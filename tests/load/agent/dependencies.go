// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package agent

import "context"

type TxGenerator[T comparable] interface {
	// GenerateTx returns a valid transaction.
	GenerateTx() (T, error)
}

type Issuer[T comparable] interface {
	// Issue sends a tx to the network, and informs the tracker that its sent
	// said transaction.
	IssueTx(ctx context.Context, tx T) error
}

type Listener[T comparable] interface {
	// Listen listens for transaction confirmations from a node, as well as
	// new blocks.
	Listen(ctx context.Context) error
	// RegisterIssued registers a transaction that was issued, so the listener
	// is aware it should track it.
	RegisterIssued(T)
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
