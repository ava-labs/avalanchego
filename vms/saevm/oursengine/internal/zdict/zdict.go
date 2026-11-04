// Copyright 2023+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style license (github.com/klauspost/compress,
// LICENSE). Derived from github.com/klauspost/compress/dict/builder.go v1.20.3.

// Package zdict builds zstd dictionaries DETERMINISTICALLY: the same samples give the same
// dictionary bytes in every process and on every machine. It is klauspost's frequency-based
// builder (dict.BuildZstdDict) with every map-order dependence removed: candidates enter the
// sort in hash order, every ranking breaks ties by hash, and the dictionary ID is the
// caller's (klauspost draws a random one). tier0 files store dictionaries built here, so
// a change to this code (or to the pinned zstd version, whose BuildDict makes the entropy
// tables) changes file bytes: it is a format version bump.
package zdict

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"math/bits"
	"slices"
	"sort"

	"github.com/klauspost/compress/zstd"
)

// hashBytes is the indexed match length (klauspost's HashBytes option; 6 measured best on
// tier0 sections).
const hashBytes = 6

type match struct {
	hash   uint32
	n      uint32
	offset int64
}

// table is an open-addressing set of 32-bit hashes (the keys are hashes already: the slot is
// the key's top bits); slot values live in the caller's arrays by slot index.
type table struct {
	keys  []uint32
	used  []bool
	n     int
	mask  int
	shift int
}

func newTable(capPow int) *table {
	return &table{keys: make([]uint32, 1<<capPow), used: make([]bool, 1<<capPow), mask: 1<<capPow - 1, shift: 32 - capPow}
}

// find returns the slot of h and whether it holds h (else the empty slot to put it in).
func (t *table) find(h uint32) (int, bool) {
	i := int(h>>t.shift) & t.mask
	for t.used[i] {
		if t.keys[i] == h {
			return i, true
		}
		i = (i + 1) & t.mask
	}
	return i, false
}

// bitsLen is the number of bits below n's top bit (log2 for powers of two).
func bitsLen(n int) int { return max(bits.Len(uint(n))-1, 0) }

// Build builds a zstd dictionary of at most size bytes of history from samples, with entropy
// tables tuned for level and the given dictionary id. It fails when the samples share no
// repeated content. Same bytes as klauspost's algorithm with deterministic order (the map
// version is kept in the tests as buildRef), with open-addressing tables instead of Go maps.
func Build(samples [][]byte, size int, level zstd.EncoderLevel, id uint32) ([]byte, error) {
	if len(samples) == 0 || size <= 0 {
		return nil, errors.New("zdict: no samples or size")
	}
	// Pass 1: per hash, the samples it occurs in and the sum of its first offsets there.
	positions := 0
	for _, b := range samples {
		positions += max(len(b)-7, 0)
	}
	if positions >= 1<<32 {
		return nil, errors.New("zdict: samples over 4 GiB") // offset sums are kept in 32 bits
	}
	ht := newTable(max(bitsLen(positions/2), 10))
	cnt, off := make([]uint32, len(ht.keys)), make([]uint32, len(ht.keys))
	var total uint64
	var seenKeys []uint32
	var seenStamp []uint32
	stamp := uint32(0)
	for _, b := range samples {
		if len(b) < 8 {
			continue
		}
		need := 1 << (bitsLen(2*len(b)) + 1)
		if need > len(seenKeys) {
			seenKeys, seenStamp, stamp = make([]uint32, need), make([]uint32, need), 0
		}
		stamp++
		smask := need - 1
		sshift := 32 - bitsLen(need)
		for i := 0; i+8 <= len(b); i++ {
			h := hash6(binary.LittleEndian.Uint64(b[i:]))
			j := int(h>>sshift) & smask
			dup := false
			for seenStamp[j] == stamp {
				if seenKeys[j] == h {
					dup = true
					break
				}
				j = (j + 1) & smask
			}
			if dup {
				continue // count the first occurrence per sample only
			}
			seenKeys[j], seenStamp[j] = h, stamp
			k, ok := ht.find(h)
			if !ok {
				if 4*(ht.n+1) > 3*len(ht.keys) {
					ht, cnt, off = rehash(ht, cnt, off)
					k, _ = ht.find(h)
				}
				ht.keys[k], ht.used[k] = h, true
				ht.n++
			}
			cnt[k]++
			off[k] += uint32(i)
			total++
		}
	}
	if ht.n == 0 {
		return nil, errors.New("zdict: no sample is 8 bytes or longer")
	}
	threshold := uint32(total / uint64(ht.n))
	var sorted []match
	for k, u := range ht.used {
		if u && cnt[k] > threshold {
			sorted = append(sorted, match{hash: ht.keys[k], n: cnt[k], offset: int64(off[k])})
		}
	}
	if len(sorted) == 0 {
		return nil, errors.New("zdict: no content is repeated more than average across samples")
	}
	offsetOf := func(h uint32) int64 { // pass 1's offset sum of a hash
		k, _ := ht.find(h)
		return int64(off[k])
	}
	// Deterministic start order, then klauspost's ranking (similar counts grouped, low
	// offsets first). The ranking is not a strict order, so its result depends on the start
	// order: fixing the start order fixes the result (Go's sort is deterministic).
	slices.SortFunc(sorted, func(a, b match) int { return cmp.Compare(a.hash, b.hash) })
	sort.Slice(sorted, func(i, j int) bool {
		deltaN := int(sorted[i].n) - int(sorted[j].n)
		if deltaN < 0 {
			deltaN = -deltaN
		}
		if uint32(deltaN) < sorted[i].n/32 {
			return sorted[i].offset < sorted[j].offset
		}
		return sorted[i].n > sorted[j].n
	})
	if len(sorted) > size {
		sorted = sorted[:size]
	}
	lowestOcc := sorted[len(sorted)-1].n
	// Candidates by index: want maps a wanted hash to its index.
	want := newTable(bitsLen(4*len(sorted)) + 1)
	widx := make([]int32, len(want.keys))
	for x, m := range sorted {
		k, _ := want.find(m.hash)
		want.keys[k], want.used[k], widx[k] = m.hash, true, int32(x)
		want.n++
	}
	idxOf := func(h uint32) int {
		if k, ok := want.find(h); ok {
			return int(widx[k])
		}
		return -1
	}
	// Pass 2: the first bytes of every candidate and the candidates following and preceding
	// it, counted as (candidate, neighbour hash) pairs.
	value := make([][hashBytes]byte, len(sorted))
	alive := make([]bool, len(sorted)) // klauspost's output map: seen in pass 2, not yet used
	follow, prec := newPairs(), newPairs()
	for _, b := range samples {
		for i := 0; i+8 <= len(b); i++ {
			x := idxOf(hash6(binary.LittleEndian.Uint64(b[i:])))
			if x < 0 {
				continue
			}
			if !alive[x] {
				alive[x] = true
				copy(value[x][:], b[i:])
			}
			if len(b)-i > hashBytes+8 {
				if hNext := hash6(binary.LittleEndian.Uint64(b[i+hashBytes:])); idxOf(hNext) >= 0 {
					follow.add(x, hNext)
				}
			}
			if i > hashBytes && len(b)-(i-hashBytes) >= 8 {
				if hPrev := hash6(binary.LittleEndian.Uint64(b[i-hashBytes:])); idxOf(hPrev) >= 0 {
					prec.add(x, hPrev)
				}
			}
		}
	}
	follows, precs := follow.lists(len(sorted)), prec.lists(len(sorted))
	live := func(h uint32) int { // klauspost: output[h] exists
		if x := idxOf(h); x >= 0 && alive[x] {
			return x
		}
		return -1
	}
	// byCount ranks candidates by count, then by offset (high first), then by hash.
	byCount := func(a, b match) int {
		if c := cmp.Compare(b.n, a.n); c != 0 {
			return c
		}
		if c := cmp.Compare(b.offset, a.offset); c != 0 {
			return c
		}
		return cmp.Compare(a.hash, b.hash)
	}
	var dst [][]byte
	added := 0
	var firstOffsets []int
	var cands []match
	for _, e := range sorted {
		if added > size {
			break
		}
		x := idxOf(e.hash)
		if !alive[x] {
			continue // already added
		}
		wantLen := max(e.n/uint32(hashBytes)/4, lowestOcc)
		tmp := make([]byte, 0, hashBytes*2)
		cands = cands[:0]
		for _, p := range precs[x] {
			if live(p.hash) >= 0 && p.n >= wantLen {
				cands = append(cands, match{hash: p.hash, n: p.n})
			}
		}
		if len(cands) > 0 {
			slices.SortFunc(cands, byCount)
			tmp = append(tmp, value[idxOf(cands[0].hash)][:]...)
		}
		tmp = append(tmp, value[x][:]...)
		alive[x] = false
		m := x
		for {
			cands = cands[:0]
			for _, p := range follows[m] {
				if live(p.hash) >= 0 {
					cands = append(cands, match{hash: p.hash, n: p.n, offset: offsetOf(p.hash)})
				}
			}
			if len(cands) == 0 {
				// Step back two bytes and continue from the string ending there, if any.
				const stepBack = 2
				if len(tmp) < hashBytes+stepBack {
					break
				}
				var t8 [8]byte
				copy(t8[:], tmp[len(tmp)-hashBytes-stepBack:])
				y := live(hash6(binary.LittleEndian.Uint64(t8[:])))
				if y < 0 || len(follows[y]) == 0 {
					break
				}
				anyLive := false // an existence test: order-free
				for _, p := range follows[y] {
					if live(p.hash) >= 0 {
						anyLive = true
						break
					}
				}
				if !anyLive {
					break
				}
				m = y
				tmp = tmp[:len(tmp)-stepBack]
				continue
			}
			slices.SortFunc(cands, byCount)
			stopAfter := cands[0].n < wantLen
			y := live(cands[0].hash)
			if y < 0 {
				break
			}
			// delete every hash inside the current string to avoid stuttering
			var toDel [16 + 8]byte
			copy(toDel[:], tmp[len(tmp)-hashBytes:])
			copy(toDel[hashBytes:], value[y][:])
			for i := range toDel[:hashBytes*2] {
				if z := idxOf(hash6(binary.LittleEndian.Uint64(toDel[i:]))); z >= 0 {
					alive[z] = false
				}
			}
			tmp = append(tmp, value[y][:]...)
			m = y
			if stopAfter {
				break
			}
		}
		if len(tmp) > hashBytes { // delete substrings already added
			for j := range tmp[:len(tmp)-hashBytes+1] {
				var t8 [8]byte
				copy(t8[:], tmp[j:])
				if z := idxOf(hash6(binary.LittleEndian.Uint64(t8[:]))); z >= 0 {
					alive[z] = false
				}
			}
		}
		dst = append(dst, tmp)
		added += len(tmp)
		if len(firstOffsets) < 3 {
			if len(tmp) > 16 {
				tmp = tmp[:16]
			}
			offCnt := make(map[int]int, len(samples))
			for _, b := range samples {
				if off := bytes.Index(b, tmp); off != -1 {
					offCnt[off]++
				}
			}
			for _, off := range firstOffsets {
				delete(offCnt, off-added)
			}
			maxCnt, maxOffset := 0, 0
			for k, v := range offCnt { // strict order: count, then the longer offset
				if v > maxCnt || v == maxCnt && k > maxOffset {
					maxCnt, maxOffset = v, k
				}
			}
			if maxCnt > 1 {
				firstOffsets = append(firstOffsets, maxOffset+added)
			}
		}
	}
	written := 0
	for i, s := range dst {
		if len(s)+written > size {
			s = s[:size-written]
		}
		dst[i] = s
		written += len(s)
		if written >= size {
			dst = dst[:i+1]
			break
		}
	}
	var hist []byte
	for i := range dst { // reverse order: the most frequent strings nearest the end
		hist = append(hist, dst[len(dst)-1-i]...)
	}
	if len(hist) < 8 {
		return nil, errors.New("zdict: dictionary history under 8 bytes")
	}
	offs := [3]int{1, 4, 8}
	for i, off := range firstOffsets {
		if i >= 3 || off == 0 || off >= len(hist) {
			break
		}
		offs[i] = off
	}
	return zstd.BuildDict(zstd.BuildDictOptions{ID: id, Contents: Groups(samples), History: hist, Offsets: offs, Level: level})
}

// rehash doubles t, moving the per-slot values along.
func rehash(t *table, cnt, off []uint32) (*table, []uint32, []uint32) {
	n := newTable(bitsLen(len(t.keys)) + 1)
	nc, no := make([]uint32, len(n.keys)), make([]uint32, len(n.keys))
	for i, u := range t.used {
		if u {
			k, _ := n.find(t.keys[i])
			n.keys[k], n.used[k], nc[k], no[k] = t.keys[i], true, cnt[i], off[i]
			n.n++
		}
	}
	return n, nc, no
}

// pairs counts (candidate index, neighbour hash) pairs.
type pairs struct {
	keys []uint64 // candidate+1 << 32 | hash; 0 = empty
	cnt  []uint32
	n    int
}

func newPairs() *pairs { return &pairs{keys: make([]uint64, 1<<16), cnt: make([]uint32, 1<<16)} }

func (p *pairs) slot(key uint64) int {
	mask := len(p.keys) - 1
	i := int((key*0x9E3779B97F4A7C15)>>32) & mask
	for p.keys[i] != 0 && p.keys[i] != key {
		i = (i + 1) & mask
	}
	return i
}

func (p *pairs) add(x int, h uint32) {
	key := uint64(x+1)<<32 | uint64(h)
	i := p.slot(key)
	if p.keys[i] == 0 {
		if 2*(p.n+1) > len(p.keys) {
			old, oc := p.keys, p.cnt
			p.keys, p.cnt = make([]uint64, 2*len(old)), make([]uint32, 2*len(old))
			for j, k := range old {
				if k != 0 {
					s := p.slot(k)
					p.keys[s], p.cnt[s] = k, oc[j]
				}
			}
			i = p.slot(key)
		}
		p.keys[i] = key
		p.n++
	}
	p.cnt[i]++
}

// lists returns per candidate its neighbours (hash, count), in hash order.
func (p *pairs) lists(n int) [][]match {
	out := make([][]match, n)
	for i, k := range p.keys {
		if k != 0 {
			x := int(k>>32) - 1
			out[x] = append(out[x], match{hash: uint32(k), n: p.cnt[i]})
		}
	}
	for _, l := range out {
		slices.SortFunc(l, func(a, b match) int { return cmp.Compare(a.hash, b.hash) })
	}
	return out
}

// maxBlock is zstd's largest block: zstd.BuildDict encodes every content as one block and
// panics on a longer one (klauspost v1.20.3, enc_base.go addBlock).
const maxBlock = 128 << 10

// Blocks splits samples longer than a zstd block into block-sized pieces, the form
// zstd.BuildDict accepts (it gathers entropy statistics only, so the split costs nothing).
func Blocks(samples [][]byte) [][]byte {
	var out [][]byte
	for _, s := range samples {
		for len(s) > maxBlock {
			out = append(out, s[:maxBlock])
			s = s[maxBlock:]
		}
		out = append(out, s)
	}
	return out
}

const prime6bytes = 227718039650203

// hash6 hashes the lowest 6 bytes of u to 32 bits.
func hash6(u uint64) uint32 { return uint32(((u << (64 - 48)) * prime6bytes) >> 32) }

// Subset keeps whole samples at an even stride until about max bytes (all of them when they
// fit): training memory and time grow with the input, the dictionary barely does past ~100x
// its size. Deterministic: a sample is kept while the kept bytes trail max/total of the bytes
// seen.
func Subset(samples [][]byte, max int) [][]byte {
	total := 0
	for _, s := range samples {
		total += len(s)
	}
	if total <= max {
		return samples
	}
	var out [][]byte
	kept, seen := 0, 0
	for _, s := range samples {
		if int64(kept)*int64(total) <= int64(seen)*int64(max) && kept+len(s) <= max {
			out = append(out, s)
			kept += len(s)
		}
		seen += len(s)
	}
	return out
}

// Groups packs samples back to back into pieces of at most a zstd block (longer samples
// split): what zstd.BuildDict gathers its entropy statistics from. Pieces the size of a frame
// match what the dictionary is used on, and BuildDict resets its encoder once per piece, not
// once per small record.
func Groups(samples [][]byte) [][]byte {
	var out [][]byte
	var cur []byte
	for _, s := range Blocks(samples) {
		if len(cur)+len(s) > maxBlock {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, s...)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
