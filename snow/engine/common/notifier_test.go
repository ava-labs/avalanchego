// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
	"testing"

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

	subscriber := NewSimpleSubscriber()
	nf := &NotificationForwarder{
		Subscribe: subscriber.WaitForEvent,
		Notifier:  Notifier(notifier),
		Log:       &logging.NoLog{},
	}

	nf.Start()
	defer nf.Close()

	go func() {
		defer wg.Done()
		subscriber.Publish(PendingTxs)
	}()

	wg.Wait()
}

func TestNotifierStopWhileSubscribing(_ *testing.T) {
	notifier := notifier(func(ctx context.Context, _ Message) error {
		<-ctx.Done()
		return nil
	})

	nf := &NotificationForwarder{
		Notifier: Notifier(notifier),
		Log:      &logging.NoLog{},
	}

	var subscribed sync.WaitGroup
	subscribed.Add(1)

	nf.Subscribe = func(ctx context.Context) (Message, error) {
		subscribed.Done()
		<-ctx.Done()
		return 0, nil
	}

	nf.Start()
	subscribed.Wait()
	nf.Close()
}

func TestNotifierStopWhileNotifying(_ *testing.T) {
	nf := &NotificationForwarder{
		Log: &logging.NoLog{},
	}

	var notifiying sync.WaitGroup
	notifiying.Add(1)

	nf.Notifier = Notifier(notifier(func(ctx context.Context, _ Message) error {
		notifiying.Wait()
		<-ctx.Done()
		return nil
	}))

	nf.Subscribe = func(context.Context) (Message, error) {
		return 0, nil
	}

	nf.Start()
	notifiying.Done()
	nf.Close()
}
