// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"github.com/ethereum/go-ethereum/common"
)

// BoundedBuffer keeps [size] common.Hash entries in a buffer and calls
// [callback] on any item that is evicted. This is typically used for
// dereferencing old roots during block processing.
type BoundedBuffer struct {
	lastPos  int
	size     int
	callback func(common.Hash)
	buffer   []common.Hash
}

// NewBoundedBuffer creates a new [BoundedBuffer].
func NewBoundedBuffer(size int, callback func(common.Hash)) *BoundedBuffer {
	return &BoundedBuffer{
		size:     size,
		callback: callback,
		buffer:   make([]common.Hash, size),
	}
}

// Insert adds a new common.Hash to the buffer. If the buffer is full, the
// oldest common.Hash will be evicted and [callback] will be invoked.
//
// WARNING: BoundedBuffer does not support the insertion of empty common.Hash.
// Inserting such data will cause unintended behavior.
func (b *BoundedBuffer) Insert(h common.Hash) {
	nextPos := (b.lastPos + 1) % b.size // the first item added to the buffer will be at position 1
	if b.buffer[nextPos] != (common.Hash{}) {
		b.callback(b.buffer[nextPos])
	}
	b.buffer[nextPos] = h
	b.lastPos = nextPos
}

// Last retrieves the last item added to the buffer.
// If no items have been added to the buffer, Last returns an empty hash.
func (b *BoundedBuffer) Last() common.Hash {
	return b.buffer[b.lastPos]
}
