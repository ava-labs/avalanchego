// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"sync/atomic"
)

var _ Haltable = (*Halter)(nil)

type Haltable interface {
	Halt(context.Context)
	Halted() bool
}

type Halter struct {
	halted uint32
}

func (h *Halter) Halt(context.Context) {
	atomic.StoreUint32(&h.halted, 1)
}

func (h *Halter) Halted() bool {
	return atomic.LoadUint32(&h.halted) == 1
}
