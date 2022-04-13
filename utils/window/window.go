// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package window

import (
	"sync"
	"time"

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
	window []node
	// head/tail pointers to mark occupied parts of the window
	head, tail int
	// how many elements are currently in the window
	size int
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
		clock:   config.Clock,
		ttl:     config.TTL,
		maxSize: config.MaxSize,
		window:  make([]node, config.MaxSize),
	}
}

// Add adds an element to a window and also evicts any elements if they've been
// present in the window beyond the configured time-to-live
func (w *Window) Add(value interface{}) {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()
	if w.size >= w.maxSize {
		w.removeOldestNode()
	}

	// add the new block id
	w.window[w.tail] = node{
		value:     value,
		entryTime: w.clock.Time(),
	}
	w.tail = (w.tail + 1) % len(w.window)
	w.size++
}

// Oldest returns the oldest element in the window.
func (w *Window) Oldest() (interface{}, bool) {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()
	if w.size == 0 {
		return nil, false
	}

	// fetch the oldest element
	return w.window[w.head].value, true
}

// Length returns the number of elements in the window.
func (w *Window) Length() int {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.removeStaleNodes()
	return w.size
}

// removeStaleNodes removes any nodes beyond the configured ttl of a window node.
func (w *Window) removeStaleNodes() {
	// If we're beyond the expiry threshold, removeStaleNodes this node from our
	// window. Nodes are guaranteed to be strictly increasing in entry time,
	// so we can break this loop once we find the first non-stale one.
	for w.size > 0 {
		if w.clock.Time().Sub(w.window[w.head].entryTime) <= w.ttl {
			break
		}

		w.removeOldestNode()
	}
}

// Removes the oldest element.
// Doesn't actually remove anything, just marks that location in memory
// as available to overwrite.
func (w *Window) removeOldestNode() {
	w.window[w.head].value = nil // mark for garbage collection
	w.head = (w.head + 1) % len(w.window)
	w.size--
}

// helper struct to represent elements in the window
type node struct {
	value     interface{}
	entryTime time.Time
}
