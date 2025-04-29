// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package agent

type Agent[T, U comparable] struct {
	TxTarget  uint64
	Generator TxGenerator[T]
	Issuer    Issuer[T]
	Listener  Listener[T]
	Tracker   Tracker[U]
}

func New[T, U comparable](
	txTarget uint64,
	generator TxGenerator[T],
	issuer Issuer[T],
	listener Listener[T],
	tracker Tracker[U],
) *Agent[T, U] {
	return &Agent[T, U]{
		TxTarget:  txTarget,
		Generator: generator,
		Issuer:    issuer,
		Listener:  listener,
		Tracker:   tracker,
	}
}
