package utils

import "sync/atomic"

type AtomicBool struct {
	value uint32
}

func (a *AtomicBool) GetValue() bool {
	return atomic.LoadUint32(&a.value) != 0
}

func (a *AtomicBool) SetValue(b bool) {
	var value uint32
	if b {
		value = 1
	}
	atomic.StoreUint32(&a.value, value)
}
