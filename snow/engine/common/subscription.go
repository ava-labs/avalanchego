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
type Subscription func(ctx context.Context) (Message, error)

// SimpleSubscriber is a basic implementation of the Subscriber interface.
// It allows publishing messages to be received by the subscriber.
// Once a message is published, it can be received via a call to WaitForEvent.
// Once Close is called, WaitForEvent always returns 0.
// It assumes there is only one subscriber at a time, and does not support concurrent subscribers,
// as a message passed by Publish is only retained until the next call to WaitForEvent.
type SimpleSubscriber struct {
	lock   sync.Mutex
	signal sync.Cond

	msg    *Message
	closed bool
}

func NewSimpleSubscriber() *SimpleSubscriber {
	ss := &SimpleSubscriber{}
	ss.signal = *sync.NewCond(&ss.lock)
	return ss
}

func (ss *SimpleSubscriber) Publish(msg Message) {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ss.msg = &msg
	ss.signal.Broadcast()
}

func (ss *SimpleSubscriber) Close() {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ss.closed = true
	ss.signal.Broadcast()
}

func (ss *SimpleSubscriber) WaitForEvent(ctx context.Context) (Message, error) {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()

		ss.lock.Lock()
		defer ss.lock.Unlock()
		ss.signal.Broadcast()
	}()

	for {
		if ss.closed {
			return 0, nil
		}

		if ss.msg != nil {
			msg := *ss.msg
			ss.msg = nil
			return msg, nil
		}

		select {
		case <-ctx.Done():
			return 0, nil
		default:
			ss.signal.Wait()
		}
	}
}

// SubscriptionProxy is a proxy that acts as a Subscriber for messages received from an underlying Subscriber.
// It gives the consumer fine-grained control over when to forward the messages received from the underlying
// subscription, as well as to inject messages to be received by the subscriber.
// In order for a message to be received by the subscriber, it must be either set explicitly using Publish, or to be received
// from the underlying subscription.
// Then, the message is only forwarded to the subscriber when the channel Forward returns is read from.
// It assumes that there is only one subscriber at a time, as messages passed by Publish or received by the underlying subscription
// are only retained until the next call to WaitForEvent.
// A call to Close will make WaitForEvent to always return 0.
type SubscriptionProxy struct {
	logger  logging.Logger
	lock    sync.Mutex
	signal  sync.Cond
	running sync.WaitGroup

	// pendingMessage is the message that is currently pending to be forwarded to the subscriber.
	pendingMessage *Message
	// forwardedMessage is the message that is ready to be forwarded to the subscriber.
	forwardedMessage *Message

	closed    bool
	subscribe Subscription
	onClose   context.CancelFunc
}

func NewSubscriptionProxy(s Subscription, logger logging.Logger) *SubscriptionProxy {
	sp := &SubscriptionProxy{
		subscribe: s,
		logger:    logger,
	}

	sp.signal = *sync.NewCond(&sp.lock)

	sp.running.Add(1)
	go func() {
		defer sp.running.Done()
		sp.proxyNotifications()
	}()

	return sp
}

func (sp *SubscriptionProxy) Close() {
	defer sp.running.Wait()

	sp.lock.Lock()
	defer sp.lock.Unlock()

	if sp.onClose != nil {
		sp.onClose()
	}

	sp.closed = true
	sp.onClose = nil

	sp.signal.Broadcast()
}

func (sp *SubscriptionProxy) isClosed() bool {
	sp.lock.Lock()
	defer sp.lock.Unlock()

	return sp.closed
}

func (sp *SubscriptionProxy) proxyNotifications() {
	for {
		if sp.isClosed() {
			return
		}
		ctx := sp.createContext()
		msg, err := sp.subscribe(ctx)
		if err != nil {
			sp.logger.Error("failed to subscribe to events", zap.Error(err))
			return
		}
		sp.Publish(msg)
		if sp.isClosed() {
			return
		}
		sp.signal.Broadcast()
	}
}

// Forward returns a channel that when read from, will forward a message received by Publish or the underlying subscription,
// to a caller of WaitForEvent. The returned channel can only be read from once.
// If the context is cancelled, or if Close is called, the channel will be closed.
func (sp *SubscriptionProxy) Forward(ctx context.Context) <-chan struct{} {
	out := make(chan struct{})

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-ctx.Done()
		sp.lock.Lock()
		defer sp.lock.Unlock()
		sp.signal.Broadcast()
	}()

	go func() {
		defer cancel()

		sp.lock.Lock()
		defer sp.lock.Unlock()

		for {
			if sp.pendingMessage != nil {
				select {
				case <-ctx.Done():
					close(out)
					return
				case out <- struct{}{}:
					sp.forwardedMessage = sp.pendingMessage
					sp.pendingMessage = nil
					sp.signal.Broadcast()
					return
				}
			}

			if sp.closed {
				close(out)
				return
			}

			select {
			case <-ctx.Done():
				close(out)
				return
			default:
				sp.signal.Wait()
			}
		}
	}()

	return out
}

func (sp *SubscriptionProxy) Publish(msg Message) {
	sp.lock.Lock()
	defer sp.lock.Unlock()

	sp.pendingMessage = &msg

	sp.signal.Broadcast()
}

func (sp *SubscriptionProxy) createContext() context.Context {
	sp.lock.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	sp.onClose = cancel
	sp.lock.Unlock()
	return ctx
}

// WaitForEvent blocks until either the given context is cancelled, or a message is received.
// In order for a message to be received, it must be either set using SetAbsorbedMsg, or to be received
// by the underlying subscription, and then the consumer must call Forward.
func (sp *SubscriptionProxy) WaitForEvent(ctx context.Context) (Message, error) {
	sp.lock.Lock()
	defer sp.lock.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		sp.lock.Lock()
		defer sp.lock.Unlock()
		sp.signal.Broadcast()
	}()

	for {
		if sp.closed {
			return 0, nil
		}

		select {
		case <-ctx.Done():
			return 0, nil
		default:
		}

		if sp.forwardedMessage != nil {
			releasedMsg := *sp.forwardedMessage
			sp.forwardedMessage = nil
			return releasedMsg, nil
		}

		select {
		case <-ctx.Done():
			return 0, nil
		default:
			sp.signal.Wait()
		}
	}
}
