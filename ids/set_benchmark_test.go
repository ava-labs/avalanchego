// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"crypto/rand"
	"testing"
)

func BenchmarkSetListSmall(b *testing.B) {
	smallLen := 5
	set := Set{}
	for i := 0; i < smallLen; i++ {
		var idBytes [32]byte
		if _, err := rand.Read(idBytes[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(ID(idBytes))
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
		var idBytes [32]byte
		if _, err := rand.Read(idBytes[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(ID(idBytes))
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
		var idBytes [32]byte
		if _, err := rand.Read(idBytes[:]); err != nil {
			b.Fatal(err)
		}
		set.Add(ID(idBytes))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		set.List()
	}
}
