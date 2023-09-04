// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

// Gossipable is an item that can be gossiped across the network
type Gossipable interface {
	// GetHash represents the unique hash of this item
	GetHash() Hash
	Marshal() ([]byte, error)
	Unmarshal(bytes []byte) error
}

// Set holds a set of known Gossipable items
type Set[T Gossipable] interface {
	// Add adds a Gossipable to the set
	Add(gossipable T) error
	// Get returns elements that match the provided filter function
	Get(filter func(gossipable T) bool) []T
	// GetFilter returns the byte representation of bloom filter and its
	// corresponding salt.
	GetFilter() (bloom []byte, salt []byte, err error)
}
