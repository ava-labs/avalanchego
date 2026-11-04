package store

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/holiman/uint256"
)

// slotCache holds Slot's answers in the current executor view. Every speculation replica and
// the committer load the slots of a tier0 again on their own (each State starts empty at a
// cut), and each load hashes the slot and searches the base's frames; the first answer serves
// the others. The view moves when Apply publishes a file (and the base under the overlay
// changes on SetBase): gen is bumped right after, under mu, and entries of an older gen are
// never served. A reader takes gen before its uncached read, so an answer read in an older
// view is stored under an older gen.
type slotCache struct {
	gen    atomic.Uint64
	shards [slotShards]slotShard
}

const slotShards = 256

type slotShard struct {
	mu  sync.Mutex
	gen uint64
	m   map[slotKey]uint256.Int
	_   [40]byte // a shard fills one 64-byte cache line: neighbouring mutexes do not share one
}

// slotKey is a storage handle and the slot as the EVM names it (before Mask and keccak).
type slotKey struct {
	h    uint32
	slot [32]byte
}

func (c *slotCache) shard(h uint32, slot *[32]byte) *slotShard {
	x := uint64(h)*0x9e3779b97f4a7c15 ^ binary.LittleEndian.Uint64(slot[24:]) ^ binary.LittleEndian.Uint64(slot[:8])
	return &c.shards[(x*0xbf58476d1ce4e5b9)>>(64-8)]
}

// get is the answer stored for (h, slot) in gen g.
func (c *slotCache) get(g uint64, h uint32, slot *[32]byte) (uint256.Int, bool) {
	sh := c.shard(h, slot)
	sh.mu.Lock()
	v, ok := sh.m[slotKey{h, *slot}]
	ok = ok && sh.gen == g
	sh.mu.Unlock()
	return v, ok
}

// put stores the answer for (h, slot) read in gen g; an answer of an older gen than the shard's
// is dropped, a newer gen empties the shard first.
func (c *slotCache) put(g uint64, h uint32, slot *[32]byte, v uint256.Int) {
	sh := c.shard(h, slot)
	sh.mu.Lock()
	switch {
	case sh.m == nil:
		sh.m, sh.gen = map[slotKey]uint256.Int{}, g
	case sh.gen < g:
		clear(sh.m)
		sh.gen = g
	}
	if sh.gen == g {
		sh.m[slotKey{h, *slot}] = v
	}
	sh.mu.Unlock()
}
