// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestMessageQueue(t *testing.T) {
	require := require.New(t)

	expectFail := false
	q := NewBlockingMessageQueue(
		SendFailedFunc(func(message.OutboundMessage) {
			require.True(expectFail)
		}),
		logging.NoLog{},
		0,
	)

	mc := newMessageCreator(t)
	msgs := []message.OutboundMessage{}
	numToSend := 10

	// Assert that the messages are popped in the same order they were pushed
	for i := 0; i < numToSend; i++ {
		m, err := mc.Ping(uint32(i))
		require.NoError(err)
		msgs = append(msgs, m)
	}

	go func() {
		for i := 0; i < numToSend; i++ {
			q.Push(context.Background(), msgs[i])
		}
	}()

	for i := 0; i < numToSend; i++ {
		msg, ok := q.Pop()
		require.True(ok)
		require.Equal(msgs[i], msg)
	}

	// Assert that PopNow returns false when the queue is empty
	_, ok := q.PopNow()
	require.False(ok)

	// Assert that Push returns false when the context is canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	expectFail = true
	gotOk := make(chan bool)
	go func() {
		gotOk <- q.Push(ctx, msgs[0])
	}()
	require.False(<-gotOk)

	// Assert that Push returns false when the queue is closed
	go func() {
		gotOk <- q.Push(context.Background(), msgs[0])
	}()
	q.Close()
	require.False(<-gotOk)

	// Assert Pop returns false when the queue is closed
	_, ok = q.Pop()
	require.False(ok)
}
