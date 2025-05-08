// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import "context"

type Issuer[T any] interface {
	// GenerateAndIssueTx generates and sends a tx to the network, and informs the
	// tracker that it sent said transaction. It returns the sent transaction.
	GenerateAndIssueTx(ctx context.Context) (tx T, err error)
}

type Listener[T any] interface {
	// Listen for the final status of transactions and notify the tracker
	// Listen stops if the context is done, an error occurs, or if it received
	// all the transactions issued and the issuer no longer issues any.
	// Listen MUST return a nil error if the context is canceled.
	Listen(ctx context.Context) error

	// RegisterIssued informs the listener that a transaction was issued.
	RegisterIssued(tx T)

	// IssuingDone informs the listener that no more transactions will be issued.
	IssuingDone()
}

// Tracker keeps track of the status of transactions.
// This must be thread-safe, so it can be called in parallel by the issuer(s) or orchestrator.
type Tracker[T any] interface {
	// Issue records a transaction that was submitted, but whose final status is
	// not yet known.
	Issue(T)
	// ObserveConfirmed records a transaction that was confirmed.
	ObserveConfirmed(T)
	// ObserveFailed records a transaction that failed (e.g. expired)
	ObserveFailed(T)

	// GetObservedIssued returns the number of transactions that the tracker has
	// confirmed were issued.
	GetObservedIssued() uint64
	// GetObservedConfirmed returns the number of transactions that the tracker has
	// confirmed were accepted.
	GetObservedConfirmed() uint64
	// GetObservedFailed returns the number of transactions that the tracker has
	// confirmed failed.
	GetObservedFailed() uint64
}

// orchestrator executes the load test by coordinating the issuers to send
// transactions, in a manner depending on the implementation.
type orchestrator interface {
	// Execute the load test
	Execute(ctx context.Context) error
}
