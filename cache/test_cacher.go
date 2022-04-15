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

// CacherTests is a list of all Cacher tests
var CacherTests = []struct {
	Size int
	Func func(t *testing.T, c Cacher)
}{
	{Size: 1, Func: TestBasic},
	{Size: 2, Func: TestEviction},
}

func TestBasic(t *testing.T, cache Cacher) {
	id1 := ids.ID{1}
	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	}

	expectedValue1 := 1
	cache.Put(id1, expectedValue1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if value != expectedValue1 {
		t.Fatalf("Failed to retrieve correct value when one exists")
	}

	cache.Put(id1, expectedValue1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if value != expectedValue1 {
		t.Fatalf("Failed to retrieve correct value when one exists")
	}

	cache.Put(id1, expectedValue1)
	if value, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if value != expectedValue1 {
		t.Fatalf("Failed to retrieve correct value when one exists")
	}

	id2 := ids.ID{2}

	expectedValue2 := 2
	cache.Put(id2, expectedValue2)
	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	}
	if value, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if value != expectedValue2 {
		t.Fatalf("Failed to retrieve correct value when one exists")
	}
}

func TestEviction(t *testing.T, cache Cacher) {
	id1 := ids.ID{1}
	id2 := ids.ID{2}
	id3 := ids.ID{3}

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
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieve value when none exists")
	}

	cache.Put(id3, 3)

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieve value when none exists")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 2 {
		t.Fatalf("Retrieved wrong value")
	} else if val, found := cache.Get(id3); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 3 {
		t.Fatalf("Retrieved wrong value")
	}

	cache.Get(id2)
	cache.Put(id1, 1)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 1 {
		t.Fatalf("Retrieved wrong value")
	} else if val, found := cache.Get(id2); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 2 {
		t.Fatalf("Retrieved wrong value")
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieved value when none exists")
	}

	cache.Evict(id2)
	cache.Put(id3, 3)

	if val, found := cache.Get(id1); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 1 {
		t.Fatalf("Retrieved wrong value")
	} else if _, found := cache.Get(id2); found {
		t.Fatalf("Retrieved value when none exists")
	} else if val, found := cache.Get(id3); !found {
		t.Fatalf("Failed to retrieve value when one exists")
	} else if val != 3 {
		t.Fatalf("Retrieved wrong value")
	}

	cache.Flush()

	if _, found := cache.Get(id1); found {
		t.Fatalf("Retrieved value when none exists")
	} else if _, found := cache.Get(id2); found {
		t.Fatalf("Retrieved value when none exists")
	} else if _, found := cache.Get(id3); found {
		t.Fatalf("Retrieved value when none exists")
	}
}
