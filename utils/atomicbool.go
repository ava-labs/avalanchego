package utils

import "sync/atomic"

type AtomicBool struct {
	value uint32
}

func (atomicBool *AtomicBool) GetValue() bool {
	return atomic.LoadUint32(&atomicBool.value) != 0
}

func (atomicBool *AtomicBool) SetValue(b bool) {
	var value uint32
	if b {
		value = 1
	} else {
		value = 0
	}
	atomic.StoreUint32(&atomicBool.value, value)
}
