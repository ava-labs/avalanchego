// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"crypto/rand"
	"strconv"
	"testing"
)

func BenchmarkSetListSmall(b *testing.B) {
	smallLen := 5
	set := Set{}
	for i := 0; i < smallLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(id)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		set.List()
	}
}

func BenchmarkSetListMedium(b *testing.B) {
	mediumLen := 25
	set := Set{}
	for i := 0; i < mediumLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(id)
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		set.List()
	}
}

func BenchmarkSetListLarge(b *testing.B) {
	largeLen := 100000
	set := Set{}
	for i := 0; i < largeLen; i++ {
		var id ID
		if _, err := rand.Read(id[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(id)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		set.List()
	}
}

func BenchmarkSetClear(b *testing.B) {
	for _, numElts := range []int{10, 25, 50, 100, 250, 500, 1000} {
		b.Run(strconv.Itoa(numElts), func(b *testing.B) {
			set := NewSet(numElts)
			for n := 0; n < b.N; n++ {
				set.Add(make([]ID, numElts)...)
				set.Clear()
			}
		})
	}
}
