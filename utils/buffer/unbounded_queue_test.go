// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnboundedSliceQueue(t *testing.T) {
	require := require.New(t)

	// Starts empty.
	bIntf := NewUnboundedSliceQueue[int](1)
	b, ok := bIntf.(*unboundedSliceQueue[int])
	require.True(ok)
	require.Equal(0, bIntf.Len())
	require.Equal(1, len(b.data))
	require.Equal(0, b.head)
	require.Equal(0, b.tail)
	// queue is [EMPTY]

	_, ok = b.Dequeue()
	require.False(ok)
	_, ok = b.PeekHead()
	require.False(ok)
	_, ok = b.PeekTail()
	require.False(ok)

	// This causes a resize
	b.Enqueue(1)
	require.Equal(1, b.Len())
	require.Equal(2, len(b.data))
	require.Equal(0, b.head)
	require.Equal(1, b.tail)
	// queue is [1,EMPTY]

	got, ok := b.PeekHead()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(1, got)

	// This causes a resize
	b.Enqueue(2)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(0, b.head)
	require.Equal(2, b.tail)
	// queue is [1,2,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(1, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(1, got)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(1, b.head)
	require.Equal(2, b.tail)
	// queue is [EMPTY,2,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(2, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(2, got)
	require.Equal(0, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.head)
	require.Equal(2, b.tail)
	// queue is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.Dequeue()
	require.False(ok)
	_, ok = b.PeekHead()
	require.False(ok)
	_, ok = b.PeekTail()
	require.False(ok)

	b.Enqueue(3)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.head)
	require.Equal(3, b.tail)
	// queue is [EMPTY,EMPTY,3,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(3, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(3, got)

	b.Enqueue(4)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.head)
	require.Equal(0, b.tail)
	// queue is [EMPTY,EMPTY,3,4]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(3, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(4, got)

	// This tests tail wrap around.
	b.Enqueue(5)
	require.Equal(3, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(2, b.head)
	require.Equal(1, b.tail)
	// queue is [5,EMPTY,3,4]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(3, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(5, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(3, got)
	require.Equal(2, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(3, b.head)
	require.Equal(1, b.tail)
	// queue is [5,EMPTY,EMPTY,4]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(4, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(5, got)

	// This tests head wrap around.
	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(4, got)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(0, b.head)
	require.Equal(1, b.tail)
	// queue is [5,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(5, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(5, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(5, got)
	require.Equal(0, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(1, b.head)
	require.Equal(1, b.tail)
	// queue is [EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.Dequeue()
	require.False(ok)
	_, ok = b.PeekHead()
	require.False(ok)
	_, ok = b.PeekTail()
	require.False(ok)

	b.Enqueue(6)
	require.Equal(1, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(1, b.head)
	require.Equal(2, b.tail)
	// queue is [EMPTY,6,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(6, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(6, got)

	b.Enqueue(7)
	require.Equal(2, b.Len())
	require.Equal(1, b.head)
	require.Equal(3, b.tail)
	// queue is [EMPTY,6,7,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(6, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(7, got)

	b.Enqueue(8)
	require.Equal(3, b.Len())
	require.Equal(4, len(b.data))
	require.Equal(1, b.head)
	require.Equal(0, b.tail)
	// queue is [EMPTY,6,7,8]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(6, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(8, got)

	// This causes a resize
	b.Enqueue(9)
	require.Equal(4, b.Len())
	require.Equal(8, len(b.data))
	require.Equal(0, b.head)
	require.Equal(4, b.tail)
	// queue is [6,7,8,9,EMPTY,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(6, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(9, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(6, got)
	// queue is [EMPTY,7,8,9,EMPTY,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(7, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(9, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(7, got)
	// queue is [EMPTY,EMPTY,8,9,EMPTY,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(8, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(9, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(8, got)
	// queue is [EMPTY,EMPTY,EMPTY,9,EMPTY,EMPTY,EMPTY,EMPTY]

	got, ok = b.PeekHead()
	require.True(ok)
	require.Equal(9, got)

	got, ok = b.PeekTail()
	require.True(ok)
	require.Equal(9, got)

	got, ok = b.Dequeue()
	require.True(ok)
	require.Equal(9, got)
	require.Equal(0, b.Len())
	require.Equal(8, len(b.data))
	require.Equal(4, b.head)
	require.Equal(4, b.tail)
	// queue is [EMPTY,EMPTY,EMPTY,EMPTY,EMPTY,EMPTY,EMPTY,EMPTY]

	_, ok = b.PeekHead()
	require.False(ok)
	_, ok = b.PeekTail()
	require.False(ok)
}
