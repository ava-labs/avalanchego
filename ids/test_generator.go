// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import "sync/atomic"

var offset = uint64(0)

// GenerateTestID returns a new ID that should only be used for testing
func GenerateTestID() ID {
	return Empty.Prefix(atomic.AddUint64(&offset, 1))
}

// GenerateTestShortID returns a new ID that should only be used for testing
func GenerateTestShortID() ShortID {
	newID := GenerateTestID()
	newShortID, _ := ToShortID(newID[:20])
	return newShortID
}

// GenerateTestShortNodeID returns a new ID that should only be used for testing
func GenerateTestShortNodeID() ShortNodeID {
	return ShortNodeID(GenerateTestShortID())
}

// GenerateTestNodeID returns a new ID that should only be used for testing
func GenerateTestNodeID() NodeID {
	return NodeIDFromShortNodeID(GenerateTestShortNodeID())
}

// BuildNodeID is an utility to build NodeID from bytes in UTs
// It must not be used in production code. In production code we should
// use ToNodeID, which performs proper length checking.
func BuildNodeID(src []byte) NodeID {
	bytes := make([]byte, LongNodeIDLen)
	copy(bytes, src)
	return NodeID{
		buf: string(bytes),
	}
}

// BuildNodeID is an utility to build NodeID from bytes in UTs
// It must not be used in production code. In production code we should
// use ToNodeID, which performs proper length checking.
func BuildShortNodeID(src []byte) NodeID {
	bytes := make([]byte, ShortNodeIDLen)
	copy(bytes, src)
	return NodeID{
		buf: string(bytes),
	}
}
