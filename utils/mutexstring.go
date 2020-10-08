package utils

import (
	"sync"
)

type MutexString struct {
	value string
	lock  sync.RWMutex
}

func (a *MutexString) GetValue() string {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.value
}

func (a *MutexString) SetValue(v string) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.value = v
}
