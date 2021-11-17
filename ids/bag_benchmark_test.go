// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"crypto/rand"
	"testing"
)

func BenchmarkBagListSmall(b *testing.B) {
	smallLen := 5
	bag := Bag{}
	for i := 0; i < smallLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		bag.Add(id)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bag.List()
	}
}

func BenchmarkBagListMedium(b *testing.B) {
	mediumLen := 25
	bag := Bag{}
	for i := 0; i < mediumLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		bag.Add(id)
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		bag.List()
	}
}

func BenchmarkBagListLarge(b *testing.B) {
	largeLen := 100000
	bag := Bag{}
	for i := 0; i < largeLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		bag.Add(id)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bag.List()
	}
}
