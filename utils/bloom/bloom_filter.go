package bloom

import (
	"fmt"
	"sync"

	"github.com/spaolacci/murmur3"

	streakKnife "github.com/holiman/bloomfilter/v2"
)

var (
	ErrMaxBytes = fmt.Errorf("too large")
)

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)

	// Check checks filter, assumed thread safe
	Check([]byte) bool
}

func New(maxN uint64, p float64, maxBytes uint64) (Filter, error) {
	neededBytes := bytesSteakKnifeFilter(maxN, p)
	if neededBytes > maxBytes {
		return nil, ErrMaxBytes
	}
	return newSteakKnifeFilter(maxN, p)
}

type steakKnifeFilter struct {
	lock    sync.RWMutex
	bfilter *streakKnife.Filter
}

func bytesSteakKnifeFilter(maxN uint64, p float64) uint64 {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	// this is pulled from bloomFilter.newBits and bloomfilter.newRandKeys
	// the calculation is the size of the bitset which would be created from this filter.
	// to ensure we don't crash memory, we would ensure the size
	msize := (m + 63) / 64
	msize += k

	// 8 == sizeof(uint64))
	return msize * 8
}

func newSteakKnifeFilter(maxN uint64, p float64) (Filter, error) {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	bfilter, err := streakKnife.New(m, k)
	return &steakKnifeFilter{bfilter: bfilter}, err
}

func (f *steakKnifeFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()
	for _, b := range bl {
		h := murmur3.New64()
		_, errf := h.Write(b)

		// this shouldnt happen
		if errf != nil {
			continue
		}
		f.bfilter.Add(h)
	}
}

func (f *steakKnifeFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()
	h := murmur3.New64()
	_, err := h.Write(b)
	if err != nil {
		return false
	}
	return f.bfilter.Contains(h)
}
