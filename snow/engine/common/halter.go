// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "sync/atomic"

var _ Haltable = (*Halter)(nil)

type Haltable interface {
	Halt()
	Halted() bool
}

type Halter struct {
	halted uint32
}

func (h *Halter) Halt() {
	atomic.StoreUint32(&h.halted, 1)
}

func (h *Halter) Halted() bool {
	return atomic.LoadUint32(&h.halted) == 1
}
