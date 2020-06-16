package ids

import (
	"crypto/rand"
	"testing"
)

//
func BenchmarkSetListSmall(b *testing.B) {
	smallLen := 5
	set := Set{}
	for i := 0; i < smallLen; i++ {
		var idBytes [32]byte
		rand.Read(idBytes[:])
		NewID(idBytes)
		set.Add(NewID(idBytes))
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
		rand.Read(idBytes[:])
		NewID(idBytes)
		set.Add(NewID(idBytes))
	}
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		set.List()
	}
}

func BenchmarkSetListLarsge(b *testing.B) {
	largeLen := 100000
	set := Set{}
	for i := 0; i < largeLen; i++ {
		var idBytes [32]byte
		rand.Read(idBytes[:])
		NewID(idBytes)
		set.Add(NewID(idBytes))
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		set.List()
	}
}
