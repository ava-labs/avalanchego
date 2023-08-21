// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "sync"

var _ sync.Locker = (*multiLock)(nil)

type multiLock struct {
	locks []sync.Locker
}

func NewMultiLock(locks ...sync.Locker) sync.Locker {
	return &multiLock{
		locks: locks,
	}
}

func (l *multiLock) Lock() {
	for _, lock := range l.locks {
		lock.Lock()
	}
}

func (l *multiLock) Unlock() {
	for _, lock := range l.locks {
		lock.Unlock()
	}
}
