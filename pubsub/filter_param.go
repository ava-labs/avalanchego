// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

type FilterParam struct {
	lock    sync.RWMutex
	address map[ids.ShortID]struct{}
	filter  bloom.Filter
}

func (f *FilterParam) ClearFilter() {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.filter = nil
}

func (f *FilterParam) NewAddresses() {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.address = make(map[ids.ShortID]struct{})
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
	f.address = nil
	return f.filter
}

func (f *FilterParam) CheckAddressID(addr2check ids.ShortID) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	if f.filter != nil && f.filter.Check(addr2check[:]) {
		return true
	}
	_, ok := f.address[addr2check]
	return ok
}

func (f *FilterParam) CheckAddress(addr2check []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	if f.filter != nil && f.filter.Check(addr2check) {
		return true
	}
	addr, err := ids.ToShortID(addr2check)
	if err != nil {
		return false
	}
	_, ok := f.address[addr]
	return ok
}

func (f *FilterParam) AddAddresses(bl ...[]byte) error {
	filter := f.Filter()
	if filter != nil {
		filter := f.Filter()
		filter.Add(bl...)
		return nil
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.address == nil {
		return ErrFilterNotInitialized
	}
	if len(f.address)+len(bl) > MaxAddresses {
		return ErrAddressLimit
	}
	for _, b := range bl {
		addr, err := ids.ToShortID(b)
		if err != nil {
			return err
		}
		f.address[addr] = struct{}{}
	}
	return nil
}

func (f *FilterParam) Len() int {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return len(f.address)
}

func NewFilterParam() *FilterParam {
	return &FilterParam{address: make(map[ids.ShortID]struct{})}
}
