// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import "github.com/ava-labs/avalanchego/ids"

// Gossipable is an item that can be gossiped across the network
type Gossipable interface {
	GetID() ids.ID
	Marshal() ([]byte, error)
	Unmarshal(bytes []byte) error
}

// Set holds a set of known Gossipable items
type Set[T Gossipable] interface {
	// Add adds a Gossipable to the set
	Add(gossipable T) error
	// Iterate iterates over elements until [f] returns false
	Iterate(f func(gossipable T) bool)
	// GetFilter returns the byte representation of bloom filter and its
	// corresponding salt.
	GetFilter() (bloom []byte, salt []byte, err error)
}
