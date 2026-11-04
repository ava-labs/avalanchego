// Package base reads the snapshot base (era pack ERASNAP2): accounts, storage slots and code
// at the base height, through an mmap of the file (the kernel page cache and readahead are
// the cache).
//
// State frames hold records sorted by (keccak(address), kind, keccak(slot)) with zeros not
// stored; a frame that starts inside an account's slots begins with a context record naming
// the account. Code frames follow, sorted by code hash. The index (77 B per frame: first key
// 65 B, offset u64, length u32) is binary searched in place.
package base

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/crypto/sha3"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// Base is an open snapshot base. Safe for concurrent use.
type Base struct {
	b     []byte
	check func(off, n uint64) error // before the first touch of a byte range (nil: none)
	parts *Parts                    // set: content and index read through Parts (OpenParts)
	done  func() error
	once  sync.Once // Close: a fold closes the base it replaced, run's defer closes it again
	cerr  error
	s     *era.Snap
	mu    sync.Mutex
	acct  []handle // storage handle h is acct[h-1]
	hOf   map[[32]byte]uint32
	// codes whose bytes were hashed to their key once (Code checks every code once per Base,
	// not on every read: Account loads its code each time, many times per block)
	codeMu sync.RWMutex
	codeOK map[[32]byte]struct{}
}

type handle struct {
	ah     [32]byte
	f0, f1 int // frames that may hold the account's slots
}

// Account is one base account. CodeHash is zero for an account without code.
type Account struct {
	Nonce     uint64
	Balance   [32]byte // big-endian
	CodeHash  [32]byte
	Code      []byte // aliases the mapping
	MultiCoin bool
	Handle    uint32 // storage handle for Slot and SlotKeys; 0: no slots in the base
}

var errCorrupt = errors.New("base: corrupt snapshot")

// Open maps the base file.
func Open(path string) (*Base, error) {
	b, done, err := era.Content(path)
	if err != nil {
		return nil, err
	}
	s, err := era.OpenSnap(b)
	if err != nil {
		done()
		return nil, err
	}
	return &Base{b: b, done: done, s: s, hOf: map[[32]byte]uint32{}}, nil
}

// OpenBytes reads a base whose content is b (a mapping the caller made). check is called with
// every byte range before it is read (the store verifies chunks there); the header, index and
// footer are checked here, once. done is called by Close.
func OpenBytes(b []byte, check func(off, n uint64) error, done func() error) (*Base, error) {
	if err := check(0, uint64(min(len(b), era.HeaderLen))); err != nil {
		return nil, err
	}
	const footer = 64
	if len(b) >= footer {
		if err := check(uint64(len(b)-footer), footer); err != nil {
			return nil, err
		}
	}
	s, err := era.OpenSnap(b)
	if err != nil {
		return nil, err
	}
	if err := check(s.IndexOf, uint64(len(b))-s.IndexOf); err != nil {
		return nil, err
	}
	return &Base{b: b, check: check, done: done, s: s, hOf: map[[32]byte]uint32{}}, nil
}

// Parts is a base whose content is not one mapped slice: a base being written (frames durable
// so far, the index entries in their own file) or one moving between files. Data returns the
// content bytes [off, off+n) and Entry index entry i (era.Entry bytes); both must stay valid
// while the caller uses the base.
type Parts struct {
	Data            func(off, n uint64) ([]byte, error)
	Entry           func(i int) []byte
	NState, NFrames int // state frames, then code frames
	Block           uint64
}

// OpenParts reads a base through p.
func OpenParts(p *Parts) *Base {
	return &Base{parts: p, done: func() error { return nil }, hOf: map[[32]byte]uint32{}}
}

func (b *Base) nState() int {
	if b.parts != nil {
		return b.parts.NState
	}
	return b.s.NState
}

func (b *Base) nFrames() int {
	if b.parts != nil {
		return b.parts.NFrames
	}
	return b.s.NFrames
}

func (b *Base) entry(i int) []byte {
	if b.parts != nil {
		return b.parts.Entry(i)
	}
	return b.b[b.s.IndexOf+uint64(i)*era.Entry:]
}

// Close unmaps the file.
func (b *Base) Close() error {
	b.once.Do(func() { b.cerr = b.done() })
	return b.cerr
}

// Snap is the base content as an era snap; its bytes alias the mapping. Read a byte range only
// after Check accepted it.
func (b *Base) Snap() *era.Snap { return b.s }

// Check verifies, on first touch, the chunks under the content range [off, off+n) (the hook
// OpenBytes got; nil for a base opened with Open, whose bytes are trusted as mapped).
func (b *Base) Check(off, n uint64) error {
	if b.check == nil {
		return nil
	}
	return b.check(off, n)
}

// Block is the height whose post-state the base holds.
func (b *Base) Block() uint64 {
	if b.parts != nil {
		return b.parts.Block
	}
	return b.s.Block
}

// Keccak is keccak256.
func Keccak(p []byte) (h [32]byte) {
	k := sha3.NewLegacyKeccak256()
	k.Write(p)
	k.Sum(h[:0])
	return h
}

// Mask is coreth's normalizeStateKey for a storage slot (bit 248 cleared).
func Mask(slot [32]byte) [32]byte {
	slot[0] &^= 1
	return slot
}

// key is the index entry's first key of frame i.
func (b *Base) key(i int) []byte { return b.entry(i)[:65] }

func (b *Base) frame(i int) ([]byte, error) {
	e := b.entry(i)
	off, n := binary.LittleEndian.Uint64(e[65:]), binary.LittleEndian.Uint32(e[73:])
	if b.parts != nil {
		return b.parts.Data(off, uint64(n))
	}
	if off+uint64(n) > uint64(len(b.b)) {
		return nil, errCorrupt
	}
	if b.check != nil {
		if err := b.check(off, uint64(n)); err != nil {
			return nil, err
		}
	}
	return b.b[off : off+uint64(n)], nil
}

// lastLE is the last state frame whose first key is <= k (-1 when none).
func (b *Base) lastLE(k []byte) int {
	return sort.Search(b.nState(), func(i int) bool { return bytes.Compare(b.key(i), k) > 0 }) - 1
}

// Account reads the base account of addr.
func (b *Base) Account(addr [20]byte) (Account, bool, error) {
	a, ok, err := b.AccountAt(Keccak(addr[:]))
	if err != nil || !ok || a.CodeHash == ([32]byte{}) {
		return a, ok, err
	}
	if a.Code, err = b.Code(a.CodeHash); err != nil {
		return Account{}, false, fmt.Errorf("base: code of %x: %w", addr, err)
	}
	return a, true, nil
}

// AccountAt reads the base account whose address hashes to ah, without its code (Code nil).
func (b *Base) AccountAt(ah [32]byte) (Account, bool, error) {
	var k [65]byte
	copy(k[:], ah[:])
	f0 := max(b.lastLE(k[:]), 0)
	k[32] = 1
	for i := 33; i < 65; i++ {
		k[i] = 0xff
	}
	f1 := max(b.lastLE(k[:]), f0)
	fb, err := b.frame(f0)
	if err != nil {
		return Account{}, false, err
	}
	var a Account
	found, slots := false, f1 > f0
	var r era.Rec
	for p := 0; p < len(fb); p += r.Len {
		if err := era.Decode(fb[p:], &r); err != nil {
			return Account{}, false, err
		}
		if r.Tag == era.TagSlot {
			if found {
				slots = true
				break
			}
			continue
		}
		if found {
			break
		}
		if r.Tag == era.TagAccount && bytes.Equal(r.Key, ah[:]) {
			found = true
			a.Nonce, a.MultiCoin = r.Nonce, r.Flags&1 != 0
			if len(r.Balance) > 32 {
				return Account{}, false, errCorrupt
			}
			copy(a.Balance[32-len(r.Balance):], r.Balance)
			if r.CodeHash != nil {
				copy(a.CodeHash[:], r.CodeHash)
			}
		}
	}
	if !found {
		if slots && b.hasSlots(ah, f0, f1) {
			return Account{}, false, fmt.Errorf("base: slots of account %x without an account record", ah)
		}
		return Account{}, false, nil
	}
	if slots && b.hasSlots(ah, f0, f1) {
		b.mu.Lock()
		if a.Handle = b.hOf[ah]; a.Handle == 0 { // one handle per account, however often it is read
			b.acct = append(b.acct, handle{ah, f0, f1})
			a.Handle = uint32(len(b.acct))
			b.hOf[ah] = a.Handle
		}
		b.mu.Unlock()
	}
	return a, true, nil
}

// hasSlots reports a slot record of ah in frames f0..f1.
func (b *Base) hasSlots(ah [32]byte, f0, f1 int) bool {
	found := false
	b.slots(handle{ah, f0, f1}, func(sh []byte, v []byte) bool { found = true; return false })
	return found
}

func (b *Base) h(h uint32) (handle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h == 0 || int(h) > len(b.acct) {
		return handle{}, fmt.Errorf("base: storage handle %d", h)
	}
	return b.acct[h-1], nil
}

// slots walks the slot records of an account in key order until f returns false.
func (b *Base) slots(c handle, f func(sh, v []byte) bool) error {
	for i := c.f0; i <= c.f1; i++ {
		fb, err := b.frame(i)
		if err != nil {
			return err
		}
		in := false
		var r era.Rec
		for p := 0; p < len(fb); p += r.Len {
			if err := era.Decode(fb[p:], &r); err != nil {
				return err
			}
			switch r.Tag {
			case era.TagAccount, era.TagContext, era.TagAbsent:
				if in {
					return nil // the next account: done
				}
				in = bytes.Equal(r.Key, c.ah[:])
			case era.TagSlot:
				if in && !f(r.Key, r.Value) {
					return nil
				}
			}
		}
	}
	return nil
}

// SlotKeys calls f with keccak(slot) of every base slot of handle h (all non-zero) until f
// returns false.
func (b *Base) SlotKeys(h uint32, f func(sh [32]byte) bool) error {
	c, err := b.h(h)
	if err != nil {
		return err
	}
	return b.slots(c, func(sh, v []byte) bool { return f([32]byte(sh)) })
}

// Slot reads slot (as the EVM sees it: masked here) of handle h; zero when absent.
func (b *Base) Slot(h uint32, slot [32]byte) ([32]byte, error) { return b.Key(h, Mask(slot)) }

// Key reads storage key k of handle h as given (a state key: a normalized EVM slot, or a
// multicoin coin id with bit 248 set); zero when absent.
func (b *Base) Key(h uint32, k32 [32]byte) ([32]byte, error) { return b.KeyAt(h, Keccak(k32[:])) }

// KeyAt is Key for the storage key whose keccak256 is sh.
func (b *Base) KeyAt(h uint32, sh [32]byte) ([32]byte, error) {
	c, err := b.h(h)
	if err != nil {
		return [32]byte{}, err
	}
	return b.keyIn(c, &sh)
}

// KeysAt is KeyAt of the keys shs[i] with need[i] (all when need is nil) of one handle, into
// out[i]: the handle is looked up (b.mu) once, not per key.
func (b *Base) KeysAt(h uint32, shs, out [][32]byte, need []bool) error {
	c, err := b.h(h)
	if err != nil {
		return err
	}
	for i := range shs {
		if need == nil || need[i] {
			if out[i], err = b.keyIn(c, &shs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Handle is a storage handle's record, for KeyIn: looked up once (b.mu), read without a lock.
type Handle struct{ c handle }

// Handle is the record of storage handle h.
func (b *Base) Handle(h uint32) (Handle, error) {
	c, err := b.h(h)
	return Handle{c}, err
}

// KeyIn is KeyAt by a handle's record: no lock (the base's mapping does not change).
func (b *Base) KeyIn(h Handle, sh *[32]byte) ([32]byte, error) { return b.keyIn(h.c, sh) }

// keyIn is the value of sh in the slots of handle c.
func (b *Base) keyIn(c handle, sh *[32]byte) ([32]byte, error) {
	var k [65]byte
	copy(k[:], c.ah[:])
	k[32] = 1
	copy(k[33:], sh[:])
	f := min(max(b.lastLE(k[:]), c.f0), c.f1)
	fb, err := b.frame(f)
	if err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	in := false
	var r era.Rec
	for p := 0; p < len(fb); p += r.Len {
		if err := era.Decode(fb[p:], &r); err != nil {
			return out, err
		}
		switch r.Tag {
		case era.TagAccount, era.TagContext, era.TagAbsent:
			if in {
				return out, nil
			}
			in = bytes.Equal(r.Key, c.ah[:])
		case era.TagSlot:
			if !in {
				continue
			}
			switch bytes.Compare(r.Key, sh[:]) {
			case 0:
				if len(r.Value) > 32 {
					return out, errCorrupt
				}
				copy(out[32-len(r.Value):], r.Value)
				return out, nil
			case 1:
				return out, nil
			}
		}
	}
	return out, nil
}

// Code reads the code with hash ch.
func (b *Base) Code(ch [32]byte) ([]byte, error) {
	lo, hi := b.nState(), b.nFrames()
	i := lo + sort.Search(hi-lo, func(i int) bool { return bytes.Compare(b.key(lo + i)[:32], ch[:]) > 0 }) - 1
	if i < lo {
		return nil, fmt.Errorf("base: code %x not found", ch)
	}
	fb, err := b.frame(i)
	if err != nil {
		return nil, err
	}
	var r era.Rec
	for p := 0; p < len(fb); p += r.Len {
		if err := era.Decode(fb[p:], &r); err != nil {
			return nil, err
		}
		if r.Tag == era.TagCode && bytes.Equal(r.Key, ch[:]) {
			b.codeMu.RLock()
			_, ok := b.codeOK[ch]
			b.codeMu.RUnlock()
			if !ok {
				if Keccak(r.Code) != ch {
					return nil, fmt.Errorf("base: code %x does not hash to its key", ch)
				}
				b.codeMu.Lock()
				if b.codeOK == nil {
					b.codeOK = map[[32]byte]struct{}{}
				}
				b.codeOK[ch] = struct{}{}
				b.codeMu.Unlock()
			}
			return r.Code, nil
		}
	}
	return nil, fmt.Errorf("base: code %x not found", ch)
}

// SlotsFrom calls f with the slots of the account whose address hashes to ah from slot key
// hash sh on (key hash and value as stored), in order, until f returns false. It reads only the
// frames from the one holding sh on, not the account's first frame: a fold's view reads the
// old base this way while the frames before sh's may be gone.
func (b *Base) SlotsFrom(ah, sh [32]byte, f func(sh, v []byte) bool) error {
	var k [65]byte
	copy(k[:], ah[:])
	k[32] = 1
	copy(k[33:], sh[:])
	for i := max(b.lastLE(k[:]), 0); i < b.nState(); i++ {
		fb, err := b.frame(i)
		if err != nil {
			return err
		}
		in := false
		var r era.Rec
		for p := 0; p < len(fb); p += r.Len {
			if err := era.Decode(fb[p:], &r); err != nil {
				return err
			}
			switch r.Tag {
			case era.TagAccount, era.TagContext, era.TagAbsent:
				c := bytes.Compare(r.Key, ah[:])
				if c > 0 {
					return nil // past the account
				}
				in = c == 0
			case era.TagSlot:
				if in && bytes.Compare(r.Key, sh[:]) >= 0 && !f(r.Key, r.Value) {
					return nil
				}
			}
		}
	}
	return nil
}
