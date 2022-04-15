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

func TestLRU(t *testing.T) {
	cache := &LRU{Size: 1}

	TestBasic(t, cache)
}

func TestLRUEviction(t *testing.T) {
	cache := &LRU{Size: 2}

	TestEviction(t, cache)
}

func TestLRUResize(t *testing.T) {
	cache := LRU{Size: 2}

	id1 := ids.ID{1}
	id2 := ids.ID{2}

	cache.Put(id1, 1)
	cache.Put(id2, 2)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 1 {
		t.Fatalf("Retrieved wrong value")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 2 {
		t.Fatalf("Retrieved wrong value")
	}

	cache.Size = 1

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 2 {
		t.Fatalf("Retrieved wrong value")
	}

	cache.Size = 0

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 2 {
		t.Fatalf("Retrieved wrong value")
	}
}
