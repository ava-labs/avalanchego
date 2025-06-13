// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"sync"
	"time"

	"github.com/ava-labs/libevm/core/types"
)

type Tracker struct {
	lock sync.RWMutex

	totalGasUsed uint64
	txsIssued    uint64
	txsAccepted  uint64
}

func (t *Tracker) LogIssued(time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsIssued++
}

func (t *Tracker) LogAccepted(receipt *types.Receipt, _ time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.txsAccepted++
	t.totalGasUsed += receipt.GasUsed
}

func (t *Tracker) TotalGasUsed() uint64 {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.totalGasUsed
}
