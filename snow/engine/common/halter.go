// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "sync/atomic"

var _ Haltable = (*Halter)(nil)

type Haltable interface {
	Halt()
	Halted() bool
}

type Halter struct {
	halted atomic.Uint32
}

func (h *Halter) Halt() {
	h.halted.Store(1)
}

func (h *Halter) Halted() bool {
	return h.halted.Load() == 1
}
