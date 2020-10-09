package utils

import (
	"sync"
)

type MutexInterface struct {
	value interface{}
	lock  sync.RWMutex
}

func NewMutexInterface(v interface{}) *MutexInterface {
	mutexInterface := MutexInterface{}
	mutexInterface.SetValue(v)
	return &mutexInterface
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
