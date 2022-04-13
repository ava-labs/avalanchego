// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
)

type evictable struct {
	id      ids.ID
	evicted int
}

func (e *evictable) Key() interface{} { return e.id }
func (e *evictable) Evict()           { e.evicted++ }

func TestEvictableLRU(t *testing.T) {
	cache := EvictableLRU{}

	expectedValue1 := &evictable{id: ids.ID{1}}
	if returnedValue := cache.Deduplicate(expectedValue1).(*evictable); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	} else if returnedValue := cache.Deduplicate(expectedValue1).(*evictable); returnedValue != expectedValue1 {
		t.Fatalf("Returned unknown value")
	} else if expectedValue1.evicted != 0 {
		t.Fatalf("Value was evicted unexpectedly")
	}

	expectedValue2 := &evictable{id: ids.ID{2}}
	returnedValue := cache.Deduplicate(expectedValue2).(*evictable)
	switch {
	case returnedValue != expectedValue2:
		t.Fatalf("Returned unknown value")
	case expectedValue1.evicted != 1:
		t.Fatalf("Value should have been evicted")
	case expectedValue2.evicted != 0:
		t.Fatalf("Value was evicted unexpectedly")
	}

	cache.Size = 2

	expectedValue3 := &evictable{id: ids.ID{2}}
	returnedValue = cache.Deduplicate(expectedValue3).(*evictable)
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
