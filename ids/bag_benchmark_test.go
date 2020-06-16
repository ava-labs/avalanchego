package ids

import (
	"crypto/rand"
	"testing"
)

//
func BenchmarkBagListSmall(b *testing.B) {
	smallLen := 5
	bag := Bag{}
	for i := 0; i < smallLen; i++ {
		var idBytes [32]byte
		rand.Read(idBytes[:])
		NewID(idBytes)
		bag.Add(NewID(idBytes))
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
		var idBytes [32]byte
		rand.Read(idBytes[:])
		NewID(idBytes)
		bag.Add(NewID(idBytes))
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		bag.List()
	}
}

func BenchmarkBagListLarsge(b *testing.B) {
	largeLen := 100000
	bag := Bag{}
	for i := 0; i < largeLen; i++ {
		var idBytes [32]byte
		rand.Read(idBytes[:])
		NewID(idBytes)
		bag.Add(NewID(idBytes))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		bag.List()
	}
}
