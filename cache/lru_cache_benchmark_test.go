package cache

import (
	"crypto/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func BenchmarkLRUCachePutSmall(b *testing.B) {
	smallLen := 5
	cache := &LRU{Size: smallLen}
	for n := 0; n < b.N; n++ {
		for i := 0; i < smallLen; i++ {
			var idBytes [32]byte
			rand.Read(idBytes[:])
			cache.Put(ids.NewID(idBytes), n)
		}
		b.StopTimer()
		cache.Flush()
		b.StartTimer()
	}
}

func BenchmarkLRUCachePutMedium(b *testing.B) {
	mediumLen := 250
	cache := &LRU{Size: mediumLen}
	for n := 0; n < b.N; n++ {
		for i := 0; i < mediumLen; i++ {
			var idBytes [32]byte
			rand.Read(idBytes[:])
			cache.Put(ids.NewID(idBytes), n)
		}
		b.StopTimer()
		cache.Flush()
		b.StartTimer()
	}
}

func BenchmarkLRUCachePutLarge(b *testing.B) {
	largeLen := 10000
	cache := &LRU{Size: largeLen}
	for n := 0; n < b.N; n++ {
		for i := 0; i < largeLen; i++ {
			var idBytes [32]byte
			rand.Read(idBytes[:])
			cache.Put(ids.NewID(idBytes), n)
		}
		b.StopTimer()
		cache.Flush()
		b.StartTimer()
	}
}
