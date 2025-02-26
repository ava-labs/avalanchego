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
		msg := subscriber.SubscribeToEvents(ctx)
		require.Equal(t, PendingTxs, msg)
	})

	t.Run("TestSubscribe before publish", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			subscriber.Publish(StateSyncDone)
		}()

		msg := subscriber.SubscribeToEvents(ctx)
		require.Equal(t, StateSyncDone, msg)
	})

	t.Run("TestSubscribe but abort", func(t *testing.T) {
		go func() {
			time.Sleep(time.Millisecond * 10)
			cancel()
		}()
		msg := subscriber.SubscribeToEvents(ctx)
		require.Equal(t, Message(0), msg)
	})

}

func TestSubscriptionDelayer(t *testing.T) {
	msgs := make(chan Message)
	subscription := func(ctx context.Context) Message {
		msg := <-msgs
		return msg
	}

	sd := NewSubscriptionDelayer(subscription)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		msg := sd.SubscribeToEvents(ctx)
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
		sd.SetAbsorbedMsg(StateSyncDone)
		sd.Absorb(ctx)
		sd.Release()
	}()

	msg := sd.SubscribeToEvents(ctx)
	require.Equal(t, StateSyncDone, msg)
}
