// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Subscription is a function that blocks until either the given context is cancelled, or a message is returned.
// It is used to receive messages from a VM such as Pending transactions, state sync completion, etc.
// The function returns the message received, or an error if the context is cancelled.
type Subscription func(ctx context.Context) (Message, error)

type Notifier interface {
	Notify(context.Context, Message) error
}

// NotificationForwarder is a component that listens for notifications from a Subscription,
// and forwards them to a Notifier.
// When PreferenceOrStateChanged is called mid-subscription, it retries the subscription.
// After Notify is called, it waits for PreferenceOrStateChanged to be called before subscribing again.
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
	for {
		nf.forwardNotification()
		select {
		case <-nf.execCtx.Done():
			return
		default:
		}
	}
}

func (nf *NotificationForwarder) forwardNotification() {
	ctx := nf.setAndGetContext()
	defer nf.cancelContext()

	nf.Log.Debug("Subscribing to notifications")

	msg, err := nf.Subscribe(ctx)
	if err != nil {
		nf.Log.Debug("Failed subscribing to notifications", zap.Error(err))
		return
	}

	nf.Log.Debug("Received notification", zap.Stringer("msg", msg))

	if err := nf.Engine.Notify(ctx, msg); err != nil {
		nf.Log.Debug("Failed notifying engine", zap.Error(err))
		return
	}

	select {
	case <-nf.execCtx.Done():
		return
	case <-ctx.Done():
	}
}

// PreferenceOrStateChanged is called whenever the block preference changes or when the engine changes state,
// and its role is to signal the NotificationForwarder to stop its current subscription and re-subscribe.
// This is needed in case a block has been accepted that changes when a VM considers the need to build a block.
// In order for the subscription to be correlated to the latest data, it needs to be retried.
func (nf *NotificationForwarder) PreferenceOrStateChanged() {
	nf.cancelContext()
}

func (nf *NotificationForwarder) cancelContext() {
	nf.lock.Lock()
	defer nf.lock.Unlock()

	if nf.abortContext != nil {
		nf.abortContext()
		nf.abortContext = nil
	}
}

func (nf *NotificationForwarder) setAndGetContext() context.Context {
	ctx, cancel := context.WithCancel(nf.execCtx)

	nf.lock.Lock()
	defer nf.lock.Unlock()
	nf.abortContext = cancel
	return ctx
}

func (nf *NotificationForwarder) Close() {
	defer nf.executing.Wait()

	nf.lock.Lock()
	defer nf.lock.Unlock()

	nf.haltExecution()
	nf.haltExecution = func() {}
}
