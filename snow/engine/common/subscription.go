// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync"
)

type Subscriber interface {
	// WaitForEvent blocks until either the given context is cancelled, or a message is returned.
	WaitForEvent(ctx context.Context) (Message, error)
}

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
