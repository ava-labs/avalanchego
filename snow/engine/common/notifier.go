// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const errThrottleTime = 100 * time.Millisecond

// Subscription is a function that blocks until either the given context is cancelled, or a message is returned.
// It is used to receive messages from a VM such as Pending transactions, state sync completion, etc.
// The function returns the message received, or an error if the context is cancelled.
type Subscription func(ctx context.Context) (Message, error)

type Notifier interface {
	Notify(context.Context, Message) error
}

// NotificationForwarder is a component that listens for notifications from a Subscription,
// and forwards them to a Notifier.
// When CheckForEvent is called mid-subscription, it retries the subscription.
// After Notify is called, it waits for CheckForEvent to be called before subscribing again.
type NotificationForwarder struct {
	Engine    Notifier
	Subscribe Subscription
	Log       logging.Logger

	lock          sync.Mutex
	executing     sync.WaitGroup
	execCtx       context.Context
	haltExecution context.CancelFunc
	abortContext  context.CancelFunc
}

func NewNotificationForwarder(
	engine Notifier,
	subscribe Subscription,
	log logging.Logger,
) *NotificationForwarder {
	nf := &NotificationForwarder{
		Engine:    engine,
		Subscribe: subscribe,
		Log:       log,
	}
	nf.start()
	return nf
}

func (nf *NotificationForwarder) start() {
	nf.executing.Add(1)
	nf.execCtx, nf.haltExecution = context.WithCancel(context.Background())
	go nf.run()
}

func (nf *NotificationForwarder) run() {
	defer nf.executing.Done()
	for nf.execCtx.Err() == nil {
		nf.forwardNotification()
	}
}

func (nf *NotificationForwarder) forwardNotification() {
	ctx := nf.setAndGetContext()
	defer nf.cancelContext()

	nf.Log.Debug("Subscribing to notifications")

	msg, err := nf.Subscribe(ctx)
	if err != nil {
		nf.Log.Debug("Failed subscribing to notifications", zap.Error(err))
		// Wait to retry
		select {
		case <-time.After(errThrottleTime):
		case <-ctx.Done():
		}
		return
	}

	nf.Log.Debug("Received notification", zap.Stringer("msg", msg))

	if err := nf.Engine.Notify(ctx, msg); err != nil {
		nf.Log.Debug("Failed notifying engine", zap.Error(err))
		// Wait to retry
		select {
		case <-time.After(errThrottleTime):
		case <-ctx.Done():
		}
		return
	}

	// Wait for the context to be cancelled before proceeding to the next subscription,
	// in order to subscribe after a block was accepted or a state sync was completed.
	<-ctx.Done()
}

// CheckForEvent cancels any outstanding WaitForEvent calls and schedules a new WaitForEvent call.
func (nf *NotificationForwarder) CheckForEvent() {
	nf.cancelContext()
}

func (nf *NotificationForwarder) cancelContext() {
	nf.lock.Lock()
	defer nf.lock.Unlock()

	if nf.abortContext != nil {
		nf.abortContext()
	}
}

func (nf *NotificationForwarder) setAndGetContext() context.Context {
	ctx, cancel := context.WithCancel(nf.execCtx)

	nf.lock.Lock()
	defer nf.lock.Unlock()
	nf.abortContext = cancel
	return ctx
}

// Close cancels any outstanding WaitForEvent calls and waits for them to return.
// After Close returns, no future WaitForEvent calls will be made by the notification forwarder.
func (nf *NotificationForwarder) Close() {
	defer nf.executing.Wait()

	nf.haltExecution()
}
