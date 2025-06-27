// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontest

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/lock"
)

// Subscriber is a basic implementation of the Subscriber interface.
//
// It allows publishing messages to be received by a subscriber.
//
// If the zero message is set as the event, all calls to WaitForEvent will
// block. If a non-zero message is set as the event, all calls to WaitForEvent
// will return with the provided message.
type Subscriber struct {
	l   sync.Mutex
	c   *lock.Cond
	msg common.Message
}

func NewSubscriber() *Subscriber {
	s := &Subscriber{}
	s.c = lock.NewCond(&s.l)
	return s
}

func (s *Subscriber) SetEvent(msg common.Message) {
	s.l.Lock()
	defer s.l.Unlock()

	s.msg = msg
	if msg != 0 {
		s.c.Broadcast()
	}
}

func (s *Subscriber) WaitForEvent(ctx context.Context) (common.Message, error) {
	s.l.Lock()
	defer s.l.Unlock()

	for s.msg == 0 {
		if err := s.c.Wait(ctx); err != nil {
			return 0, err
		}
	}
	return s.msg, nil
}
