// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSimpleSubscriber(t *testing.T) {
	subscriber := NewSimpleSubscriber()
	ctx, cancel := context.WithCancel(context.Background())

	t.Run("TestSubscribe after publish", func(t *testing.T) {
		subscriber.Publish(PendingTxs)
		msg, _ := subscriber.WaitForEvent(ctx)
		require.Equal(t, PendingTxs, msg)
	})

	t.Run("TestSubscribe before publish", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			subscriber.Publish(StateSyncDone)
		}()

		msg, _ := subscriber.WaitForEvent(ctx)
		require.Equal(t, StateSyncDone, msg)
	})

	t.Run("TestSubscribe but abort", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			cancel()
		}()
		msg, _ := subscriber.WaitForEvent(ctx)
		require.Equal(t, Message(0), msg)
	})
}

func TestSimpleSubscriberClose(t *testing.T) {
	subscriber := NewSimpleSubscriber()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		msg, _ := subscriber.WaitForEvent(ctx)
		require.Equal(t, Message(0), msg)
	}()

	subscriber.Close()
}
