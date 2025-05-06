// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

type Agent[T, U comparable] struct {
	Issuer   Issuer[T]
	Listener Listener[T]
	Tracker  Tracker[U]
}

func NewAgent[T, U comparable](
	issuer Issuer[T],
	listener Listener[T],
	tracker Tracker[U],
) Agent[T, U] {
	return Agent[T, U]{
		Issuer:   issuer,
		Listener: listener,
		Tracker:  tracker,
	}
}

func GetTotalObservedConfirmed[T, U comparable](agents []Agent[T, U]) uint64 {
	total := uint64(0)
	for _, agent := range agents {
		total += agent.Tracker.GetObservedConfirmed()
	}
	return total
}
