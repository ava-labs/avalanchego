// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

type evictable[K comparable] struct {
	id      K
	evicted int
}

func (e *evictable[K]) Key() K {
	return e.id
}

func (e *evictable[_]) Evict() {
	e.evicted++
}

func TestEvictableLRU(t *testing.T) {
	cache := EvictableLRU[ids.ID, *evictable[ids.ID]]{}

	expectedValue1 := &evictable[ids.ID]{id: ids.ID{1}}
	if returnedValue := cache.Deduplicate(expectedValue1); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	} else if returnedValue := cache.Deduplicate(expectedValue1); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}

	expectedValue2 := &evictable[ids.ID]{id: ids.ID{2}}
	returnedValue := cache.Deduplicate(expectedValue2)
	switch {
	case returnedValue != expectedValue2:
		t.Fatalf("Returned unknown value")
	case expectedValue1.evicted != 1:
		t.Fatalf("Value should have been evicted")
	case expectedValue2.evicted != 0:
		t.Fatalf("Value was evicted unexpectedly")
	}

	cache.Size = 2

	expectedValue3 := &evictable[ids.ID]{id: ids.ID{2}}
	returnedValue = cache.Deduplicate(expectedValue3)
	switch {
	case returnedValue != expectedValue2:
		t.Fatalf("Returned unknown value")
	case expectedValue1.evicted != 1:
		t.Fatalf("Value should have been evicted")
	case expectedValue2.evicted != 0:
		t.Fatalf("Value was evicted unexpectedly")
	}

	cache.Flush()
	switch {
	case expectedValue1.evicted != 1:
		t.Fatalf("Value should have been evicted")
	case expectedValue2.evicted != 1:
		t.Fatalf("Value should have been evicted")
	case expectedValue3.evicted != 0:
		t.Fatalf("Value was evicted unexpectedly")
	}
}
