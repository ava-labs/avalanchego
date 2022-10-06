// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"crypto/rand"
	"strconv"
	"testing"
)

func BenchmarkSetList(b *testing.B) {
	sizes := []int{5, 25, 100, 100_000} // Test with various sizes
	for size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			set := Set[testSettable]{}
			for i := 0; i < size; i++ {
				var s testSettable
				if _, err := rand.Read(s[:]); err != nil {
					b.Fatal(err)
				}
				set.Add(s)
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				set.List()
			}
		})
	}
}

func BenchmarkSetClear(b *testing.B) {
	for _, numElts := range []int{10, 25, 50, 100, 250, 500, 1000} {
		b.Run(strconv.Itoa(numElts), func(b *testing.B) {
			set := NewSet[testSettable](numElts)
			for n := 0; n < b.N; n++ {
				set.Add(make([]testSettable, numElts)...)
				set.Clear()
			}
		})
	}
}
