// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

// BoundedBuffer keeps [size] entries of type [K] in a buffer and calls
// [callback] on any item that is overwritten. This is typically used for
// dereferencing old roots during block processing.
//
// BoundedBuffer is not thread-safe and requires the caller synchronize usage.
type BoundedBuffer[K any] struct {
	lastPos  int
	size     int
	callback func(K) error
	buffer   []K

	cycled bool
}

// NewBoundedBuffer creates a new [BoundedBuffer].
func NewBoundedBuffer[K any](size int, callback func(K) error) *BoundedBuffer[K] {
	return &BoundedBuffer[K]{
		lastPos:  -1,
		size:     size,
		callback: callback,
		buffer:   make([]K, size),
	}
}

// Insert adds a new value to the buffer. If the buffer is full, the
// oldest value will be overwritten and [callback] will be invoked.
func (b *BoundedBuffer[K]) Insert(h K) error {
	nextPos := b.lastPos + 1 // the first item added to the buffer will be at position 0
	if nextPos == b.size {
		nextPos = 0
		// Set [cycled] since we are back to the 0th element
		b.cycled = true
	}
	if b.cycled {
		// We ensure we have cycled through the buffer once before invoking the
		// [callback] to ensure we don't call it with unset values.
		if err := b.callback(b.buffer[nextPos]); err != nil {
			return err
		}
	}
	b.buffer[nextPos] = h
	b.lastPos = nextPos
	return nil
}

// Last retrieves the last item added to the buffer.
//
// If no items have been added to the buffer, Last returns the default value of
// [K] and [false].
func (b *BoundedBuffer[K]) Last() (K, bool) {
	if b.lastPos == -1 {
		return *new(K), false
	}
	return b.buffer[b.lastPos], true
}
