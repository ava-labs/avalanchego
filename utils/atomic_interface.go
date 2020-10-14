package utils

import (
	"sync"
)

type AtomicInterface struct {
	value interface{}
	lock  sync.RWMutex
}

func NewAtomicInterface(v interface{}) *AtomicInterface {
	mutexInterface := AtomicInterface{}
	mutexInterface.SetValue(v)
	return &mutexInterface
}

func (a *AtomicInterface) GetValue() interface{} {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.value
}

func (a *AtomicInterface) SetValue(v interface{}) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.value = v
}
