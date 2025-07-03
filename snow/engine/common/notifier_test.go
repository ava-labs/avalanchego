// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type notifier func(_ context.Context, msg Message) error

func (n notifier) Notify(ctx context.Context, msg Message) error {
	return n(ctx, msg)
}

func TestNotifier(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)

	notifier := notifier(func(_ context.Context, msg Message) error {
		defer wg.Done()
		require.Equal(t, PendingTxs, msg)
		return nil
	})

	c := make(chan Message)

	subscriber := func(ctx context.Context) (Message, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case msg := <-c:
			return msg, nil
		}
	}

	nf := NewNotificationForwarder(
		Notifier(notifier),
		subscriber,
		&logging.NoLog{})

	defer nf.Close()

	go func() {
		defer wg.Done()
		c <- PendingTxs
	}()

	wg.Wait()
}

func TestNotifierStopWhileSubscribing(_ *testing.T) {
	notifier := notifier(func(ctx context.Context, _ Message) error {
		<-ctx.Done()
		return nil
	})

	var subscribed sync.WaitGroup
	subscribed.Add(1)

	subscribe := func(ctx context.Context) (Message, error) {
		subscribed.Done()
		<-ctx.Done()
		return 0, nil
	}

	nf := NewNotificationForwarder(
		Notifier(notifier),
		subscribe,
		&logging.NoLog{})

	subscribed.Wait()
	nf.Close()
}

func TestNotifierWaitForPrefChangeAfterNotify(t *testing.T) {
	var notifiedCount uint32

	engine := Notifier(notifier(func(_ context.Context, _ Message) error {
		atomic.AddUint32(&notifiedCount, 1)
		return nil
	}))

	subscribe := func(context.Context) (Message, error) {
		return 0, nil
	}

	nf := NewNotificationForwarder(engine, subscribe, &logging.NoLog{})
	defer nf.Close()

	require.Eventually(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(1)
	}, time.Minute, 10*time.Millisecond)

	require.Never(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(2)
	}, time.Millisecond*100, 10*time.Millisecond)

	nf.PreferenceOrStateChanged()

	require.Eventually(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(2)
	}, time.Minute, 10*time.Millisecond)

	require.Never(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(3)
	}, time.Millisecond*100, 10*time.Millisecond)
}

func TestNotifierReSubscribeAtPrefChange(t *testing.T) {
	c := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)

	nf := &NotificationForwarder{
		Log: &logging.NoLog{},
	}

	var subscribedCount uint32

	nf.Subscribe = func(ctx context.Context) (Message, error) {
		if atomic.AddUint32(&subscribedCount, 1) == 1 {
			nf.PreferenceOrStateChanged()
		}

		select {
		case <-ctx.Done():
			close(c)
			return 0, ctx.Err()
		case <-c:
			wg.Done()
		}
		return PendingTxs, nil
	}

	var notifiedCount uint32

	nf.Engine = Notifier(notifier(func(_ context.Context, _ Message) error {
		atomic.AddUint32(&notifiedCount, 1)
		return nil
	}))

	nf.start()
	defer nf.Close()

	wg.Wait()

	require.Eventually(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(1)
	}, time.Minute, 10*time.Millisecond)

	require.Never(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == uint32(2)
	}, time.Millisecond*100, 10*time.Millisecond)
}
