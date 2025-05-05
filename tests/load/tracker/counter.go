// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"github.com/ava-labs/libevm/common"
)

// Counter only counts the number of transactions confirmed or failed,
// so that it is accessible through Go code.
type Counter struct {
	confirmed uint64
	failed    uint64
}

func NewCounter() *Counter {
	return &Counter{}
}

func (*Counter) Issue(_ common.Hash)              {}
func (c *Counter) ObserveConfirmed(_ common.Hash) { c.confirmed++ }
func (c *Counter) ObserveFailed(_ common.Hash)    { c.failed++ }
func (*Counter) ObserveBlock(_ uint64)            {}
func (c *Counter) GetObservedConfirmed() uint64   { return c.confirmed }
func (c *Counter) GetObservedFailed() uint64      { return c.failed }
func (*Counter) Log()                             {}
