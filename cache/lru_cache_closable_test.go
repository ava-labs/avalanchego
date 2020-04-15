// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"reflect"
	"testing"

	"github.com/ava-labs/gecko/ids"
)

type testCloser struct {
	value  int
	closed bool
}

func (tc *testCloser) Close() {
	tc.closed = true
}

func TestLRUCloser(t *testing.T) {
	cache := LRUCloser{Size: 1}

	id1 := ids.NewID([32]byte{1})
	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	}

	closer1 := &testCloser{1, false}

	cache.Put(id1, closer1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(value, closer1) {
		t.Fatalf("Failed to retrieve correct value when one exists")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Put(id1, closer1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(value, closer1) {
		t.Fatalf("Failed to retrieve correct value when one exists")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Put(id1, closer1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(value, closer1) {
		t.Fatalf("Failed to retrieve correct value when one exists")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	}

	id2 := ids.NewID([32]byte{2})

	closer2 := &testCloser{2, false}
	cache.Put(id2, closer2)
	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	} else if !closer1.closed {
		t.Fatalf("should have been closed when evicted")
	} else if value, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(value, closer2) {
		t.Fatalf("Failed to retrieve correct value when one exists")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	}
}

func TestLRUCloserEviction(t *testing.T) {
	cache := LRUCloser{Size: 2}

	id1 := ids.NewID([32]byte{1})
	closer1 := &testCloser{1, false}
	id2 := ids.NewID([32]byte{2})
	closer2 := &testCloser{2, false}
	id3 := ids.NewID([32]byte{3})
	closer3 := &testCloser{3, false}

	cache.Put(id1, closer1)
	cache.Put(id2, closer2)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer1) {
		t.Fatalf("Retrieved wrong value")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieve value when none exists")
	}

	cache.Put(id3, closer3) // id1 should get evicted

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if !closer1.closed {
		t.Fatalf("should have closed when value was evicted")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	} else if val, found := cache.Get(id3); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer3) {
		t.Fatalf("Retrieved wrong value")
	} else if closer3.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Get(id2)
	closer1 = &testCloser{1, false}
	cache.Put(id1, closer1) // id3 should be evicted

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer1) {
		t.Fatalf("Retrieved wrong value")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieved value when none exists")
	} else if !closer3.closed {
		t.Fatalf("should have closed when value was evicted")
	}

	cache.Evict(id2)
	closer3 = &testCloser{3, false}
	cache.Put(id3, closer3)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer1) {
		t.Fatalf("Retrieved wrong value")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	} else if _, found := cache.Get(id2); found {
		t.Fatalf("Retrieved value when none exists")
	} else if !closer2.closed {
		t.Fatalf("should have closed when value was evicted")
	} else if val, found := cache.Get(id3); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer3) {
		t.Fatalf("Retrieved wrong value")
	} else if closer3.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Flush()

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	} else if _, found := cache.Get(id2); found {
		t.Fatalf("Retrieved value when none exists")
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieved value when none exists")
	}

	if !closer1.closed || !closer2.closed || !closer3.closed {
		t.Fatal("all values should have been closed")
	}
}

func TestLRUCloserResize(t *testing.T) {
	cache := LRUCloser{Size: 2}

	id1 := ids.NewID([32]byte{1})
	closer1 := &testCloser{1, false}
	id2 := ids.NewID([32]byte{2})
	closer2 := &testCloser{2, false}

	cache.Put(id1, closer1)
	cache.Put(id2, closer2)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer1) {
		t.Fatalf("Retrieved wrong value")
	} else if closer1.closed {
		t.Fatal("shouldn't have been closed")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Size = 1

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if !closer1.closed {
		t.Fatal("should have closed when value was evicted")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	}

	cache.Size = 0

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if !reflect.DeepEqual(val, closer2) {
		t.Fatalf("Retrieved wrong value")
	} else if closer2.closed {
		t.Fatal("shouldn't have been closed")
	}
}
