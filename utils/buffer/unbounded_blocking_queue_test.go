// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnboundedBlockingQueueEnqueue(t *testing.T) {
	require := require.New(t)

	queue := NewUnboundedSliceQueue[int](1)
	blockingQueue := NewUnboundedBlockingQueue(queue)

	ok := blockingQueue.Enqueue(1)
	require.True(ok)
	ok = blockingQueue.Enqueue(2)
	require.True(ok)

	ch, ok := blockingQueue.Dequeue()
	require.True(ok)
	require.Equal(1, ch)
}

func TestUnboundedBlockingQueueDequeue(t *testing.T) {
	require := require.New(t)

	queue := NewUnboundedSliceQueue[int](1)
	blockingQueue := NewUnboundedBlockingQueue(queue)

	ok := blockingQueue.Enqueue(1)
	require.True(ok)

	ch, ok := blockingQueue.Dequeue()
	require.True(ok)
	require.Equal(1, ch)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		ch, ok := blockingQueue.Dequeue()
		require.True(ok)
		require.Equal(2, ch)
		wg.Done()
	}()

	ok = blockingQueue.Enqueue(2)
	require.True(ok)
	wg.Wait()
}

func TestUnboundedBlockingQueueClose(t *testing.T) {
	require := require.New(t)

	queue := NewUnboundedSliceQueue[int](1)
	blockingQueue := NewUnboundedBlockingQueue(queue)

	ok := blockingQueue.Enqueue(1)
	require.True(ok)

	blockingQueue.Close()

	_, ok = blockingQueue.Dequeue()
	require.False(ok)

	ok = blockingQueue.Enqueue(1)
	require.False(ok)
}
