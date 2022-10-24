// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnboundedDeque_InitialCapGreaterThanMin(t *testing.T) {
	require := require.New(t)

	bIntf := NewUnboundedDeque[int](10)
	b, ok := bIntf.(*unboundedSliceDeque[int])
	require.True(ok)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	got, ok := b.PopLeft()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	got, ok = b.PopRight()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushRight(1)
	require.Equal(1, b.Len())
	got, ok = b.PopRight()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushRight(1)
	require.Equal(1, b.Len())
	got, ok = b.PopLeft()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	b.PushLeft(2)
	require.Equal(2, b.Len())
	got, ok = b.PopLeft()
	require.Equal(1, b.Len())
	require.True(ok)
	require.Equal(2, got)
	got, ok = b.PopLeft()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushRight(1)
	require.Equal(1, b.Len())
	b.PushRight(2)
	require.Equal(2, b.Len())
	got, ok = b.PopRight()
	require.Equal(1, b.Len())
	require.True(ok)
	require.Equal(2, got)
	got, ok = b.PopRight()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(1, got)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	b.PushLeft(2)
	require.Equal(2, b.Len())
	got, ok = b.PopRight()
	require.Equal(1, b.Len())
	require.True(ok)
	require.Equal(1, got)
	got, ok = b.PopLeft()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(2, got)

	b.PushRight(1)
	require.Equal(1, b.Len())
	b.PushLeft(2)
	require.Equal(2, b.Len())
	got, ok = b.PopRight()
	require.Equal(1, b.Len())
	require.True(ok)
	require.Equal(1, got)
	got, ok = b.PopLeft()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(2, got)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	b.PushRight(2)
	require.Equal(2, b.Len())
	got, ok = b.PopLeft()
	require.Equal(1, b.Len())
	require.True(ok)
	require.Equal(1, got)
	got, ok = b.PopRight()
	require.Equal(0, b.Len())
	require.True(ok)
	require.Equal(2, got)
}

// Cases we test:
// 1. [left] moves to the left (no wrap around).
// 2. [left] moves to the right (no wrap around).
// 3. [left] wrapping around to the left side.
// 4. [left] wrapping around to the right side.
// 5. Resize.
func TestUnboundedSliceDequePushLeftPopLeft(t *testing.T) {
	require := require.New(t)

	// Starts empty.
	bIntf := NewUnboundedDeque[int](2)
	b, ok := bIntf.(*unboundedSliceDeque[int])
	require.True(ok)
	require.Equal(0, bIntf.Len())
	require.Equal(2, len(b.data))
	require.Equal(0, b.left)
	require.Equal(1, b.right)
	// slice is [EMPTY]

	_, ok = b.PopLeft()
	require.False(ok)
	_, ok = b.PeekLeft()
	require.False(ok)
	_, ok = b.PeekRight()
	require.False(ok)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	require.Equal(2, len(b.data))
	require.Equal(1, b.left)
	require.Equal(1, b.right)
	// slice is [1,EMPTY]

	got, ok := b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// This causes a resize
	b.PushLeft(2)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(2, b.right)
	// slice is [2,1,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// Tests left moving left with no wrap around.
	b.PushLeft(3)
	require.Equal(3, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.left)
	require.Equal(2, b.right)
	// slice is [2,1,EMPTY,3]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(3, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// Tests left moving right with no wrap around.
	got, ok = b.PopLeft()
	require.True(ok)
	require.Equal(3, got)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(2, b.right)
	// slice is [2,1,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// Tests left wrapping around to the left side.
	got, ok = b.PopLeft()
	require.True(ok)
	require.Equal(2, got)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(0, b.left)
	require.Equal(2, b.right)
	// slice is [EMPTY,1,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// Test left wrapping around to the right side.
	b.PushLeft(2)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(2, b.right)
	// slice is [2,1,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PopLeft()
	require.True(ok)
	require.Equal(2, got)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(0, b.left)
	require.Equal(2, b.right)
	// slice is [EMPTY,1,EMPTY,EMPTY]

	got, ok = b.PopLeft()
	require.True(ok)
	require.Equal(1, got)
	require.Equal(0, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(1, b.left)
	require.Equal(2, b.right)
	// slice is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.PopLeft()
	require.False(ok)
	_, ok = b.PeekLeft()
	require.False(ok)
	_, ok = b.PeekRight()
	require.False(ok)
}

func TestUnboundedSliceQueuePushRightPopRight(t *testing.T) {
	require := require.New(t)

	// Starts empty.
	bIntf := NewUnboundedDeque[int](2)
	b, ok := bIntf.(*unboundedSliceDeque[int])
	require.True(ok)
	require.Equal(0, bIntf.Len())
	require.Equal(2, len(b.data))
	require.Equal(0, b.left)
	require.Equal(1, b.right)
	// slice is [EMPTY]

	_, ok = b.PopRight()
	require.False(ok)
	_, ok = b.PeekLeft()
	require.False(ok)
	_, ok = b.PeekRight()
	require.False(ok)

	b.PushRight(1)
	require.Equal(1, b.Len())
	require.Equal(2, len(b.data))
	require.Equal(0, b.left)
	require.Equal(0, b.right)
	// slice is [1,EMPTY]

	got, ok := b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// This causes a resize
	b.PushRight(2)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(2, b.right)
	// slice is [1,2,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(2, got)

	// Tests right moving right with no wrap around
	b.PushRight(3)
	require.Equal(3, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(3, b.right)
	// slice is [1,2,3,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(3, got)

	// Tests right moving left with no wrap around
	got, ok = b.PopRight()
	require.True(ok)
	require.Equal(3, got)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(2, b.right)
	// slice is [1,2,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PopRight()
	require.True(ok)
	require.Equal(2, got)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(1, b.right)
	// slice is [1,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PopRight()
	require.True(ok)
	require.Equal(1, got)
	require.Equal(0, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.left)
	require.Equal(0, b.right)
	// slice is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.PeekLeft()
	require.False(ok)
	_, ok = b.PeekRight()
	require.False(ok)
	_, ok = b.PopRight()
	require.False(ok)

	b.PushLeft(1)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.left)
	require.Equal(0, b.right)
	// slice is [EMPTY,EMPTY,EMPTY,1]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(1, got)

	// Test right wrapping around to the right
	got, ok = b.PopRight()
	require.True(ok)
	require.Equal(1, got)
	require.Equal(0, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.left)
	require.Equal(3, b.right)
	// slice is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.PeekLeft()
	require.False(ok)

	_, ok = b.PeekRight()
	require.False(ok)

	// Tests right wrapping around to the left
	b.PushRight(2)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.left)
	require.Equal(0, b.right)
	// slice is [EMPTY,EMPTY,EMPTY,1]

	got, ok = b.PeekLeft()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PeekRight()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PopRight()
	require.True(ok)
	require.Equal(2, got)
	// slice is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.PeekLeft()
	require.False(ok)
	_, ok = b.PeekRight()
	require.False(ok)
	_, ok = b.PopRight()
	require.False(ok)
}
