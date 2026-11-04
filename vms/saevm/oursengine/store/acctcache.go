package store

import (
	"sync"
	"sync/atomic"

	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/asmevm"
)

// acctCache holds Account's answers in the current executor view. Its generation is bumped
// right after every publish and base switch, as slotCache's, and also after every Settle (under
// mu): a Settle that prunes an account's last slot versions turns its handle to 0 while every
// value stays. Every speculation
// replica and the committer load each account of a tier0 again on their own (each State starts
// empty at a cut); an uncached load reads the overlay, the base account and its code (copied),
// and MultiCoin read all of it a second time. The first answer serves the others. A reader takes
// the generation before its uncached read, so an answer read in an older view is stored under
// an older generation and never served.
type acctCache struct {
	gen    atomic.Uint64
	shards [acctShards]acctShard
}

const acctShards = 64

type acctShard struct {
	mu  sync.Mutex
	gen uint64
	m   map[[20]byte]acctAns
}

// acctAns is one Account answer and the account's multicoin flag in the same view.
type acctAns struct {
	ok        bool
	nonce     uint64
	balance   uint256.Int
	codeHash  asmevm.Hash
	code      []byte // shared by every State that loads it: code is never changed in place
	h         uint32
	multicoin bool
}

func (c *acctCache) shard(addr *[20]byte) *acctShard {
	return &c.shards[(uint(addr[0])<<8|uint(addr[19]))%acctShards]
}

// get is the answer stored for addr in gen g.
func (c *acctCache) get(g uint64, addr *[20]byte) (acctAns, bool) {
	sh := c.shard(addr)
	sh.mu.Lock()
	a, ok := sh.m[*addr]
	ok = ok && sh.gen == g
	sh.mu.Unlock()
	return a, ok
}

// put stores the answer for addr read in gen g; an answer of an older gen than the shard's is
// dropped, a newer gen empties the shard first.
func (c *acctCache) put(g uint64, addr *[20]byte, a acctAns) {
	sh := c.shard(addr)
	sh.mu.Lock()
	switch {
	case sh.m == nil:
		sh.m, sh.gen = map[[20]byte]acctAns{}, g
	case sh.gen < g:
		clear(sh.m)
		sh.gen = g
	}
	if sh.gen == g {
		sh.m[*addr] = a
	}
	sh.mu.Unlock()
}
