// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type Subscriber interface {
	// WaitForEvent blocks until either the given context is cancelled, or a
	// message is returned.
	WaitForEvent(context.Context) (Message, error)
}

type Notifier interface {
	Notify(context.Context, Message) error
}

// NotificationForwarder is a component that listens for notifications from a Subscription,
// and forwards them to a Notifier.
type NotificationForwarder struct {
	log        logging.Logger
	subscriber Subscriber
	notifier   Notifier

	ctx     context.Context
	closer  context.CancelFunc
	running sync.WaitGroup

	lock          sync.Mutex
	currentCtx    context.Context
	cancelCurrent context.CancelFunc
}

func NewNotificationForwarder(
	log logging.Logger,
	subscriber Subscriber,
	notifier Notifier,
) *NotificationForwarder {
	ctx, closer := context.WithCancel(context.Background())
	currentCtx, cancelCurrent := context.WithCancel(ctx)
	nf := &NotificationForwarder{
		log:        log,
		subscriber: subscriber,
		notifier:   notifier,

		ctx:    ctx,
		closer: closer,

		currentCtx:    currentCtx,
		cancelCurrent: cancelCurrent,
	}

	nf.running.Add(1)
	go nf.run(currentCtx)
	return nf
}

func (nf *NotificationForwarder) run(ctx context.Context) {
	defer nf.running.Done()

	nf.log.Warn("waiting for first VM request")

	// Wait for CheckForEvent or Close to be called at least once.
	<-ctx.Done()

	for nf.ctx.Err() == nil {
		nf.lock.Lock()
		ctx := nf.currentCtx
		nf.lock.Unlock()

		nf.log.Warn("waiting for event")

		msg, err := nf.subscriber.WaitForEvent(ctx)
		if ctx.Err() != nil {
			nf.log.Warn("waiting for event cancelled",
				zap.Error(ctx.Err()),
			)
			// If the long-lived or short-lived context was cancelled, we should
			// continue.
			continue
		}
		if err != nil {
			nf.log.Warn("error returned by wait for event",
				zap.Error(err),
			)
			// TODO: Rather than spinning on an unexpected error, we should
			// probably have a backoff here.
			continue
		}

		nf.log.Warn("notifying engine of VM message",
			zap.Stringer("msg", msg),
		)

		if err := nf.notifier.Notify(nf.ctx, msg); err != nil {
			nf.log.Warn("error returned by notify",
				zap.Error(err),
			)
		}

		nf.log.Warn("waiting for next VM request")

		// We should wait until a new context is provided before waiting for
		// another event.
		<-ctx.Done()
	}
}

// CheckForEvent schedules a new call to WaitForEvent. If there is a current
// call to WaitForEvent, the current call is cancelled before the new call is
// initiated.
func (nf *NotificationForwarder) CheckForEvent() {
	nf.lock.Lock()
	defer nf.lock.Unlock()

	nf.cancelCurrent()
	nf.currentCtx, nf.cancelCurrent = context.WithCancel(nf.ctx)
}

// Close cancels any current calls to WaitForEvent and returns once no more
// calls to WaitForEvent will be made.
func (nf *NotificationForwarder) Close() {
	nf.closer()
	nf.running.Wait()
}
