// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

import (
	"io"
	"sync"
)

// Closer ...
type Closer struct {
	closers []io.Closer
	lock    sync.Mutex
}

// Add ...
func (c *Closer) Add(closer io.Closer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.closers = append(c.closers, closer)
}

// Close closes each of the closers add to [c] and returns the first error
//  that occurs or nil if no error occurs.
func (c *Closer) Close() error {
	c.lock.Lock()
	defer c.lock.Unlock()

	closers := c.closers
	c.closers = nil
	errs := Errs{}
	for _, closer := range closers {
		errs.Add(closer.Close())
	}
	return errs.Err
}
