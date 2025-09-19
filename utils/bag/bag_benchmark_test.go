// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"math/rand"
	"testing"
)

func BenchmarkBagListSmall(b *testing.B) {
	rand := rand.New(rand.NewSource(1337)) //#nosec G404
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
	rand := rand.New(rand.NewSource(1337)) //#nosec G404
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
	rand := rand.New(rand.NewSource(1337)) //#nosec G404
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
