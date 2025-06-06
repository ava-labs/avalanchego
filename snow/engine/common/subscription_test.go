// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestSimpleSubscriber(t *testing.T) {
	subscriber := NewSimpleSubscriber()
	ctx, cancel := context.WithCancel(context.Background())

	t.Run("TestSubscribe after publish", func(t *testing.T) {
		subscriber.Publish(PendingTxs)
		msg, height := subscriber.SubscribeToEvents(ctx, 5000)
		require.Equal(t, PendingTxs, msg)
		require.Equal(t, uint64(5000), height)
	})

	t.Run("TestSubscribe before publish", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			subscriber.Publish(StateSyncDone)
		}()

		msg, _ := subscriber.SubscribeToEvents(ctx, 0)
		require.Equal(t, StateSyncDone, msg)
	})

	t.Run("TestSubscribe but abort", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			cancel()
		}()
		msg, _ := subscriber.SubscribeToEvents(ctx, 0)
		require.Equal(t, Message(0), msg)
	})

}

func TestSubscriptionDelayer(t *testing.T) {
	msgs := make(chan Message)
	subscription := func(ctx context.Context, uint65 uint64) (Message, uint64) {
		select {
		case msg := <-msgs:
			return msg, 0
		case <-ctx.Done():
			return Message(0), 0
		}
	}

	sd := NewSubscriptionDelayer(subscription)
	defer sd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		msg, _ := sd.SubscribeToEvents(ctx, 0)
		require.Equal(t, PendingTxs, msg)
		require.True(t, time.Since(start) > time.Millisecond*20)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond * 10)
		msgs <- PendingTxs
	}()

	sd.Absorb(ctx)
	require.True(t, time.Since(start) > time.Millisecond*10)
	time.Sleep(time.Millisecond * 10)
	sd.Release()

	wg.Wait()

	go func() {
		time.Sleep(time.Millisecond * 10)
		sd.SetAbsorbedMsgAndHeight(StateSyncDone, 0)
		sd.Absorb(ctx)
		sd.Release()
	}()

	msg, _ := sd.SubscribeToEvents(ctx, 0)
	require.Equal(t, StateSyncDone, msg)
}
