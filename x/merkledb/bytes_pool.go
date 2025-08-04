// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "sync"

type bytesPool struct {
	slots     chan struct{}
	bytesLock sync.Mutex
	bytes     [][]byte
}

func newBytesPool(numSlots int) *bytesPool {
	return &bytesPool{
		slots: make(chan struct{}, numSlots),
		bytes: make([][]byte, 0, numSlots),
	}
}

func (p *bytesPool) Acquire() []byte {
	p.slots <- struct{}{}
	return p.pop()
}

func (p *bytesPool) TryAcquire() ([]byte, bool) {
	select {
	case p.slots <- struct{}{}:
		return p.pop(), true
	default:
		return nil, false
	}
}

func (p *bytesPool) pop() []byte {
	p.bytesLock.Lock()
	defer p.bytesLock.Unlock()

	numBytes := len(p.bytes)
	if numBytes == 0 {
		return nil
	}

	b := p.bytes[numBytes-1]
	p.bytes = p.bytes[:numBytes-1]
	return b
}

func (p *bytesPool) Release(b []byte) {
	// Before waking anyone waiting on a slot, return the bytes.
	p.bytesLock.Lock()
	p.bytes = append(p.bytes, b)
	p.bytesLock.Unlock()

	select {
	case <-p.slots:
	default:
		panic("release of unacquired semaphore")
	}
}
