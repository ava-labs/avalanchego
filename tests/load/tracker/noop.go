// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"github.com/ava-labs/libevm/common"
)

// Noop is a no-op implementation of the tracker.
type Noop struct{}

func NewNoop() *Noop {
	return &Noop{}
}

func (Noop) Issue(common.Hash)            {}
func (Noop) ObserveConfirmed(common.Hash) {}
func (Noop) ObserveFailed(common.Hash)    {}
func (Noop) ObserveBlock(uint64)          {}
func (Noop) GetObservedConfirmed() uint64 { return 0 }
func (Noop) GetObservedFailed() uint64    { return 0 }
func (Noop) Log()                         {}
