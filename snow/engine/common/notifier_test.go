// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	wg.Add(1)

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

	c <- PendingTxs
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
		return atomic.LoadUint32(&notifiedCount) == 1
	}, time.Minute, 10*time.Millisecond)

	require.Never(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) != 1
	}, time.Millisecond*100, 10*time.Millisecond)

	nf.CheckForEvent()

	require.Eventually(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) == 2
	}, time.Minute, 10*time.Millisecond)

	require.Never(t, func() bool {
		return atomic.LoadUint32(&notifiedCount) != 2
	}, time.Millisecond*100, 10*time.Millisecond)
}

func TestNotifierReSubscribeAtPrefChange(t *testing.T) {
	notifications := make(chan Message)

	engine := Notifier(notifier(func(ctx context.Context, msg Message) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notifications <- msg:
		}

		<-ctx.Done()
		return ctx.Err()
	}))

	messages := make(chan Message)

	signal := make(chan struct{})

	subscriber := func(ctx context.Context) (Message, error) {
		select {
		case <-signal:
		default:
			close(signal)
			<-ctx.Done()
			return 0, ctx.Err()
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case msg := <-messages:
			return msg, nil
		}
	}

	nf := NewNotificationForwarder(engine, subscriber, &logging.NoLog{})
	defer nf.Close()

	select {
	case <-signal:
		nf.CheckForEvent()
	case <-time.After(time.Minute):
		require.FailNow(t, "Timed out waiting for the notification forwarder to subscribe")
	}

	select {
	case messages <- PendingTxs:
	case <-time.After(time.Minute):
		require.FailNow(t, "Timed out waiting to send message to notification forwarder")
	}

	select {
	case msg := <-notifications:
		require.Equal(t, PendingTxs, msg)
	case <-time.After(time.Minute):
		require.FailNow(t, "Timed out waiting for notification forwarder to forward message")
	}
}
