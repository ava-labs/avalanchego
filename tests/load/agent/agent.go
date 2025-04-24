// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package agent

type Agent[T, U comparable] struct {
	TxTarget  uint64
	Generator TxGenerator[T]
	Issuer    Issuer[T]
	Tracker   Tracker[U]
}

func New[T, U comparable](
	txTarget uint64,
	generator TxGenerator[T],
	issuer Issuer[T],
	tracker Tracker[U],
) *Agent[T, U] {
	return &Agent[T, U]{
		TxTarget:  txTarget,
		Generator: generator,
		Issuer:    issuer,
		Tracker:   tracker,
	}
}
