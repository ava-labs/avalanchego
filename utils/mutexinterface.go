package utils

import (
	"sync"
)

type MutexInterface struct {
	value interface{}
	lock  sync.RWMutex
}

func (a *MutexInterface) GetValue() interface{} {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.value
}

func (a *MutexInterface) SetValue(v interface{}) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.value = v
}
