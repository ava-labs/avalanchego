// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consistent

// Hashable is an interface to be implemented by structs that need to be sharded via consistent hashing.
type Hashable interface {
	// ConsistentHashKey is the key used to shard the blob.
	// This should be constant for a given blob.
	ConsistentHashKey() []byte
}
