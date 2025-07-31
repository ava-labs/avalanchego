// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"math/rand"
	"testing"
)

func init() {
	rand.Seed(1337) // for determinism
}

func BenchmarkBagListSmall(b *testing.B) {
	smallLen := 5
	bag := Bag[int]{}
	for i := 0; i < smallLen; i++ {
		bag.Add(rand.Int()) // #nosec G404
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bag.List()
	}
}

func BenchmarkBagListMedium(b *testing.B) {
	mediumLen := 25
	bag := Bag[int]{}
	for i := 0; i < mediumLen; i++ {
		bag.Add(rand.Int()) // #nosec G404
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		bag.List()
	}
}

func BenchmarkBagListLarge(b *testing.B) {
	largeLen := 100000
	bag := Bag[int]{}
	for i := 0; i < largeLen; i++ {
		bag.Add(rand.Int()) // #nosec G404
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bag.List()
	}
}
