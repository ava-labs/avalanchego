// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const initialQueueSize = 64

var (
	_ MessageQueue = (*throttledMessageQueue)(nil)
	_ MessageQueue = (*blockingMessageQueue)(nil)
)

type SendFailedCallback interface {
	SendFailed(*message.OutboundMessage)
}

type SendFailedFunc func(*message.OutboundMessage)

func (f SendFailedFunc) SendFailed(msg *message.OutboundMessage) {
	f(msg)
}

type MessageQueue interface {
	// Push attempts to add the message to the queue. If the context is
	// canceled, then pushing the message will return `false` and the message
	// will not be added to the queue.
	Push(ctx context.Context, msg *message.OutboundMessage) bool

	// Pop blocks until a message is available and then returns the message. If
	// the queue is closed, then `false` is returned.
	Pop() (*message.OutboundMessage, bool)
	// PopNow attempts to return a message without blocking. If a message is not
	// available or the queue is closed, then `false` is returned.
	PopNow() (*message.OutboundMessage, bool)

	// Close empties the queue and prevents further messages from being pushed
	// onto it. After calling close once, future calls to close will do nothing.
	Close()
}

type throttledMessageQueue struct {
	onFailed SendFailedCallback
	// [id] of the peer we're sending messages to
	id                   ids.NodeID
	log                  logging.Logger
	outboundMsgThrottler throttling.OutboundMsgThrottler

	// Signalled when a message is added to the queue and when Close() is
	// called.
	cond *sync.Cond

	// closed flags whether the send queue has been closed.
	// [cond.L] must be held while accessing [closed].
	closed bool

	// queue of the messages
	// [cond.L] must be held while accessing [queue].
	queue buffer.Deque[*message.OutboundMessage]
}

func NewThrottledMessageQueue(
	onFailed SendFailedCallback,
	id ids.NodeID,
	log logging.Logger,
	outboundMsgThrottler throttling.OutboundMsgThrottler,
) MessageQueue {
	return &throttledMessageQueue{
		onFailed:             onFailed,
		id:                   id,
		log:                  log,
		outboundMsgThrottler: outboundMsgThrottler,
		cond:                 sync.NewCond(&sync.Mutex{}),
		queue:                buffer.NewUnboundedDeque[*message.OutboundMessage](initialQueueSize),
	}
}

func (q *throttledMessageQueue) Push(ctx context.Context, msg *message.OutboundMessage) bool {
	if err := ctx.Err(); err != nil {
		q.log.Debug(
			"dropping outgoing message",
			zap.Stringer("messageOp", msg.Op),
			zap.Stringer("nodeID", q.id),
			zap.Error(err),
		)
		q.onFailed.SendFailed(msg)
		return false
	}

	// Acquire space on the outbound message queue, or drop [msg] if we can't.
	if !q.outboundMsgThrottler.Acquire(msg, q.id) {
		q.log.Debug(
			"dropping outgoing message",
			zap.String("reason", "rate-limiting"),
			zap.Stringer("messageOp", msg.Op),
			zap.Stringer("nodeID", q.id),
		)
		q.onFailed.SendFailed(msg)
		return false
	}

	// Invariant: must call q.outboundMsgThrottler.Release(msg, q.id) when [msg]
	// is popped or, if this queue closes before [msg] is popped, when this
	// queue closes.

	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		q.log.Debug(
			"dropping outgoing message",
			zap.String("reason", "closed queue"),
			zap.Stringer("messageOp", msg.Op),
			zap.Stringer("nodeID", q.id),
		)
		q.outboundMsgThrottler.Release(msg, q.id)
		q.onFailed.SendFailed(msg)
		return false
	}

	q.queue.PushRight(msg)
	q.cond.Signal()
	return true
}

func (q *throttledMessageQueue) Pop() (*message.OutboundMessage, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	for {
		if q.closed {
			return nil, false
		}
		if q.queue.Len() > 0 {
			// There is a message
			break
		}
		// Wait until there is a message
		q.cond.Wait()
	}

	return q.pop(), true
}

func (q *throttledMessageQueue) PopNow() (*message.OutboundMessage, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed || q.queue.Len() == 0 {
		// There isn't a message
		return nil, false
	}

	return q.pop(), true
}

func (q *throttledMessageQueue) pop() *message.OutboundMessage {
	msg, _ := q.queue.PopLeft()

	q.outboundMsgThrottler.Release(msg, q.id)
	return msg
}

func (q *throttledMessageQueue) Close() {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	if q.closed {
		return
	}

	q.closed = true

	for q.queue.Len() > 0 {
		msg, _ := q.queue.PopLeft()
		q.outboundMsgThrottler.Release(msg, q.id)
		q.onFailed.SendFailed(msg)
	}
	q.queue = nil

	q.cond.Broadcast()
}

type blockingMessageQueue struct {
	onFailed SendFailedCallback
	log      logging.Logger

	closeOnce   sync.Once
	closingLock sync.RWMutex
	closing     chan struct{}

	// queue of the messages
	queue chan *message.OutboundMessage
}

func NewBlockingMessageQueue(
	onFailed SendFailedCallback,
	log logging.Logger,
	bufferSize int,
) MessageQueue {
	return &blockingMessageQueue{
		onFailed: onFailed,
		log:      log,

		closing: make(chan struct{}),
		queue:   make(chan *message.OutboundMessage, bufferSize),
	}
}

func (q *blockingMessageQueue) Push(ctx context.Context, msg *message.OutboundMessage) bool {
	q.closingLock.RLock()
	defer q.closingLock.RUnlock()

	ctxDone := ctx.Done()
	select {
	case <-q.closing:
		q.log.Debug(
			"dropping message",
			zap.String("reason", "closed queue"),
			zap.Stringer("messageOp", msg.Op),
		)
		q.onFailed.SendFailed(msg)
		return false
	case <-ctxDone:
		q.log.Debug(
			"dropping message",
			zap.String("reason", "cancelled context"),
			zap.Stringer("messageOp", msg.Op),
		)
		q.onFailed.SendFailed(msg)
		return false
	default:
	}

	select {
	case q.queue <- msg:
		return true
	case <-ctxDone:
		q.log.Debug(
			"dropping message",
			zap.String("reason", "cancelled context"),
			zap.Stringer("messageOp", msg.Op),
		)
		q.onFailed.SendFailed(msg)
		return false
	case <-q.closing:
		q.log.Debug(
			"dropping message",
			zap.String("reason", "closed queue"),
			zap.Stringer("messageOp", msg.Op),
		)
		q.onFailed.SendFailed(msg)
		return false
	}
}

func (q *blockingMessageQueue) Pop() (*message.OutboundMessage, bool) {
	select {
	case msg := <-q.queue:
		return msg, true
	case <-q.closing:
		return nil, false
	}
}

func (q *blockingMessageQueue) PopNow() (*message.OutboundMessage, bool) {
	select {
	case msg := <-q.queue:
		return msg, true
	default:
		return nil, false
	}
}

func (q *blockingMessageQueue) Close() {
	q.closeOnce.Do(func() {
		close(q.closing)

		q.closingLock.Lock()
		defer q.closingLock.Unlock()

		for {
			select {
			case msg := <-q.queue:
				q.onFailed.SendFailed(msg)
			default:
				return
			}
		}
	})
}
