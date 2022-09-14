// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package window

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// Window is an interface which represents a sliding window of elements.
type Window struct {
	// mocked clock for unit testing
	clock *mockable.Clock
	// time-to-live for elements in the window
	ttl time.Duration
	// max amount of elements allowed in the window
	maxSize int

	// mutex for synchronization
	lock sync.Mutex
	// elements in the window
	elements buffer.UnboundedQueue[node]
}

// Config exposes parameters for Window
type Config struct {
	Clock   *mockable.Clock
	MaxSize int
	TTL     time.Duration
}

// New returns an instance of window
func New(config Config) *Window {
	return &Window{
		clock:    config.Clock,
		ttl:      config.TTL,
		maxSize:  config.MaxSize,
		elements: buffer.NewUnboundedSliceQueue[node](config.MaxSize + 1),
	}
}

// Add adds an element to a window and also evicts any elements if they've been
// present in the window beyond the configured time-to-live
func (w *Window) Add(value interface{}) {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()
	if w.elements.Len() >= w.maxSize {
		_, _ = w.elements.Dequeue()
	}

	// add the new block id
	w.elements.Enqueue(node{
		value:     value,
		entryTime: w.clock.Time(),
	})
}

// Oldest returns the oldest element in the window.
func (w *Window) Oldest() (interface{}, bool) {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()

	oldest, ok := w.elements.PeekHead()
	if !ok {
		return nil, false
	}
	return oldest.value, true
}

// Length returns the number of elements in the window.
func (w *Window) Length() int {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()
	return w.elements.Len()
}

// removeStaleNodes removes any nodes beyond the configured ttl of a window node.
func (w *Window) removeStaleNodes() {
	// If we're beyond the expiry threshold, removeStaleNodes this node from our
	// window. Nodes are guaranteed to be strictly increasing in entry time,
	// so we can break this loop once we find the first non-stale one.
	for {
		oldest, ok := w.elements.PeekHead()
		if !ok || w.clock.Time().Sub(oldest.entryTime) <= w.ttl {
			return
		}
		_, _ = w.elements.Dequeue()
	}
}

// helper struct to represent elements in the window
type node struct {
	value     interface{}
	entryTime time.Time
}
