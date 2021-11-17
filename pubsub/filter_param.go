// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/bloom"
)

type FilterParam struct {
	lock   sync.RWMutex
	set    map[string]struct{}
	filter bloom.Filter
}

func NewFilterParam() *FilterParam {
	return &FilterParam{
		set: make(map[string]struct{}),
	}
}

func (f *FilterParam) NewSet() {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.set = make(map[string]struct{})
	f.filter = nil
}

func (f *FilterParam) Filter() bloom.Filter {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.filter
}

func (f *FilterParam) SetFilter(filter bloom.Filter) bloom.Filter {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.filter = filter
	f.set = nil
	return f.filter
}

func (f *FilterParam) Check(addr []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()

	if f.filter != nil && f.filter.Check(addr) {
		return true
	}
	_, ok := f.set[string(addr)]
	return ok
}

func (f *FilterParam) Add(bl ...[]byte) error {
	filter := f.Filter()
	if filter != nil {
		filter.Add(bl...)
		return nil
	}

	f.lock.Lock()
	defer f.lock.Unlock()

	if f.set == nil {
		return ErrFilterNotInitialized
	}

	if len(f.set)+len(bl) > MaxAddresses {
		return ErrAddressLimit
	}
	for _, b := range bl {
		f.set[string(b)] = struct{}{}
	}
	return nil
}

func (f *FilterParam) Len() int {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return len(f.set)
}
