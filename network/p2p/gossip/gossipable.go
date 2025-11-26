// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import "github.com/ava-labs/avalanchego/ids"

// Gossipable is an item that can be gossiped across the network
type Gossipable interface {
	GossipID() ids.ID
}

// Marshaller handles parsing logic for a concrete Gossipable type
type Marshaller[T Gossipable] interface {
	MarshalGossip(T) ([]byte, error)
	UnmarshalGossip([]byte) (T, error)
}

// Set holds a set of known Gossipable items
type Set[T Gossipable] interface {
	// Add adds a Gossipable to the set. If the Gossipable is already in the
	// set, no error should be returned. Otherwise, if the Gossipable was not
	// added to the set, an error should be returned.
	Add(gossipable T) error
	// Has returns true if the gossipable is in the set.
	Has(gossipID ids.ID) bool
	// Iterate iterates over elements until [f] returns false
	Iterate(f func(gossipable T) bool)
	// GetFilter returns the byte representation of bloom filter and its
	// corresponding salt.
	GetFilter() (bloom []byte, salt []byte)
}
