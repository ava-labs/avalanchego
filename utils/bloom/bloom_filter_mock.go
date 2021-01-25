package bloom

import (
	"encoding/hex"
	"fmt"
	"sync"
)

type mockBloomFilter struct {
	lock   sync.RWMutex
	values map[string]struct{}
}

func NewMock() Filter {
	return &mockBloomFilter{values: make(map[string]struct{})}
}

func (f *mockBloomFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, b := range bl {
		f.values[hex.EncodeToString(b)] = struct{}{}
	}
}

func (f *mockBloomFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	_, exists := f.values[hex.EncodeToString(b)]
	return exists
}

func (f *mockBloomFilter) MarshalJSON() ([]byte, error) {
	return []byte(""), fmt.Errorf("unimplemented")
}
func (f *mockBloomFilter) MarshalText() ([]byte, error) {
	return []byte(""), fmt.Errorf("unimplemented")
}
