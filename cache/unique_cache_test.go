// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/ava-labs/avalanche-go/ids"
)

type evictable struct {
	id      ids.ID
	evicted int
}

func (e *evictable) ID() ids.ID { return e.id }
func (e *evictable) Evict()     { e.evicted++ }

func TestEvictableLRU(t *testing.T) {
	cache := EvictableLRU{}

	expectedValue1 := &evictable{id: ids.NewID([32]byte{1})}
	if returnedValue := cache.Deduplicate(expectedValue1).(*evictable); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	} else if returnedValue := cache.Deduplicate(expectedValue1).(*evictable); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}

	expectedValue2 := &evictable{id: ids.NewID([32]byte{2})}
	if returnedValue := cache.Deduplicate(expectedValue2).(*evictable); returnedValue != expectedValue2 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 1 {
		t.Fatalf("Value should have been evicted")
	} else if expectedValue2.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}

	cache.Size = 2

	expectedValue3 := &evictable{id: ids.NewID([32]byte{2})}
	if returnedValue := cache.Deduplicate(expectedValue3).(*evictable); returnedValue != expectedValue2 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 1 {
		t.Fatalf("Value should have been evicted")
	} else if expectedValue2.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}

	cache.Flush()
	if expectedValue1.evicted != 1 {
		t.Fatalf("Value should have been evicted")
	} else if expectedValue2.evicted != 1 {
		t.Fatalf("Value should have been evicted")
	} else if expectedValue3.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}
}
