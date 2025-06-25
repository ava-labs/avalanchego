// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
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

func TestSubscriptionProxy(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		f             func(*SubscriptionProxy, context.CancelFunc, chan Message)
		expectedEvent Message
	}{
		{
			name: "Natural notification",
			f: func(_ *SubscriptionProxy, _ context.CancelFunc, msgs chan Message) {
				msgs <- PendingTxs
			},
			expectedEvent: PendingTxs,
		},
		{
			name: "Inject notification after delay",
			f: func(sp *SubscriptionProxy, _ context.CancelFunc, _ chan Message) {
				sp.Publish(StateSyncDone)
			},
			expectedEvent: StateSyncDone,
		},
		{
			name: "Cancel wait",
			f: func(_ *SubscriptionProxy, cancel context.CancelFunc, _ chan Message) {
				cancel()
			},
		},
		{
			name: "Close",
			f: func(sp *SubscriptionProxy, _ context.CancelFunc, _ chan Message) {
				sp.Close()
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			msgs := make(chan Message)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			subscription := func(ctx context.Context) (Message, error) {
				select {
				case msg := <-msgs:
					return msg, nil
				case <-ctx.Done():
					return Message(0), nil
				}
			}

			sp := NewSubscriptionProxy(subscription, &logging.NoLog{})

			start := time.Now()

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				time.Sleep(time.Millisecond * 10)
				testCase.f(sp, cancel, msgs)
			}()

			go func() {
				defer wg.Done()
				<-sp.Forward(ctx)
				require.Greater(t, time.Since(start), time.Millisecond*10)
			}()

			msg, _ := sp.WaitForEvent(ctx)
			require.Greater(t, time.Since(start), time.Millisecond*10)
			require.Equal(t, testCase.expectedEvent, msg)
			wg.Wait()
		})
	}
}
