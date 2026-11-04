package asmevm

import (
	"container/list"
	"sync"
	"time"
	"unsafe"

	"github.com/holiman/uint256"
)

// The decoded-code cache: one process-wide LRU of asm-core streams keyed by code hash, bounded
// by a byte budget, safe for concurrent States (speculation workers). An account keeps the
// stream it fetched for its State's life (one lookup per contract per State, not per tx: the
// executor starts its States again at every cut), so an evicted stream lives on only while a
// State that ran it does; the cache bounds the rest. Streams are immutable once built, so
// threads share them.

// DefaultDecodeCacheBytes is the default byte budget (SetDecodeCacheBytes changes it): the
// smallest whole MB with at least 95% hits on the 10k window 96646201-96656200 (26,078 txs,
// asm core, every stream built lazily, receipts exact; Mac, 2026-10-22):
//
//	budget   hits     replay
//	16 MB    65.93%   7.43 s
//	64 MB    89.88%   3.51 s
//	96 MB    93.90%   2.91 s
//	112 MB   94.92%   2.72 s
//	113 MB   94.99%   2.75 s
//	114 MB   95.05%   3.30 s (noisy run; 116 MB: 95.15%, 2.75 s)
//	128 MB   95.71%   2.80 s
//	256 MB   98.07%   2.27 s
//	no bound 98.28%   2.55 s, 351 MB held
//
// A miss costs ~120 μs (building a stream of 16 B per code byte). Repro:
// DECCACHE=1 DECCACHE_MB=113,114 go test -tags asmcore -run TestDecodeCacheWindow -v ./asmevm/
const DefaultDecodeCacheBytes = 114 << 20

type decKey struct {
	hash Hash
	aot  bool // built with the native AOT entries (nativeAOTOn)
}

type decEntry struct {
	key  decKey
	d    *decoded
	size int64
}

var decCache = struct {
	sync.Mutex
	budget, used int64
	m            map[decKey]*list.Element
	lru          list.List // front: most recent
	hits, misses uint64
	missTime     time.Duration
}{budget: DefaultDecodeCacheBytes, m: map[decKey]*list.Element{}}

// decodedSize is the heap a stream holds (slices plus the struct).
func decodedSize(d *decoded) int64 {
	w := int64(unsafe.Sizeof(uint256.Int{}))
	return int64(len(d.handlers)+len(d.args))*8 + int64(cap(d.consts)+cap(d.consts2))*w + int64(unsafe.Sizeof(*d)) + 96
}

// cachedDecode returns the stream of code (hash h), from the cache or built and inserted.
func cachedDecode(h Hash, code []byte) *decoded {
	k := decKey{h, nativeAOTOn}
	c := &decCache
	c.Lock()
	if e, ok := c.m[k]; ok {
		c.lru.MoveToFront(e)
		c.hits++
		d := e.Value.(*decEntry).d
		c.Unlock()
		return d
	}
	c.misses++
	c.Unlock()
	t0 := time.Now()
	d := predecodeHook(code) // outside the lock: two threads may build the same stream, the first insert wins
	dt := time.Since(t0)
	size := decodedSize(d)
	c.Lock()
	defer c.Unlock()
	c.missTime += dt
	if e, ok := c.m[k]; ok {
		c.lru.MoveToFront(e)
		return e.Value.(*decEntry).d
	}
	if size > c.budget {
		return d // larger than the whole budget: used, not kept
	}
	for c.used+size > c.budget {
		old := c.lru.Back()
		oe := old.Value.(*decEntry)
		c.lru.Remove(old)
		delete(c.m, oe.key)
		c.used -= oe.size
	}
	c.m[k] = c.lru.PushFront(&decEntry{k, d, size})
	c.used += size
	return d
}

// SetDecodeCacheBytes sets the byte budget and empties the cache and its counters.
func SetDecodeCacheBytes(n int64) {
	c := &decCache
	c.Lock()
	defer c.Unlock()
	c.budget, c.used = n, 0
	c.m = map[decKey]*list.Element{}
	c.lru.Init()
	c.hits, c.misses, c.missTime = 0, 0, 0
}

// DecodeCacheStats reports hits, misses, the time spent building missed streams and the bytes
// held.
func DecodeCacheStats() (hits, misses uint64, missTime time.Duration, bytes int64) {
	c := &decCache
	c.Lock()
	defer c.Unlock()
	return c.hits, c.misses, c.missTime, c.used
}
