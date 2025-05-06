// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package agent

type Agent[T, U comparable] struct {
	TxTarget uint64
	Issuer   Issuer[T]
	Listener Listener[T]
	Tracker  Tracker[U]
}

func New[T, U comparable](
	txTarget uint64,
	issuer Issuer[T],
	listener Listener[T],
	tracker Tracker[U],
) *Agent[T, U] {
	return &Agent[T, U]{
		TxTarget: txTarget,
		Issuer:   issuer,
		Listener: listener,
		Tracker:  tracker,
	}
}
