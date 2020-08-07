// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"math/rand"
	"sort"
	"testing"
)

func TestSubsetUniform(t *testing.T) {
	s := &Uniform{N: 5}
	subset := Subset(s, 5)
	if len(subset) != 5 {
		t.Fatalf("Returned wrong number of elements")
	}
	sort.Ints(subset)

	for i := 0; i < 5; i++ {
		if i != subset[i] {
			t.Fatalf("Returned wrong element")
		}
	}
}
func TestSubsetWeighted(t *testing.T) {
	rand.Seed(5)
	s := &Weighted{Weights: []uint64{1, 2, 3, 4, 5}}
	subset := Subset(s, 5)
	if len(subset) != 5 {
		t.Fatalf("Returned wrong number of elements")
	}
	sort.Ints(subset)

	for i := 0; i < 5; i++ {
		if i != subset[i] {
			t.Fatalf("Returned wrong element")
		}
	}
}
