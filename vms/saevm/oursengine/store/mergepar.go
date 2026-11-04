package store

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"runtime"
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
)

// mergeParMin is the run size (both inputs' records) from which mergePar splits the work, and
// mergeParMax the most goroutines it uses.
var mergeParMin = 1 << 16

const mergeParMax = 16

// mergePar is merge(a, b, k), on up to mergeParMax goroutines for large runs. The key space is cut
// at keys of the larger run; every version of a key lies on one side of a cut in both runs, so
// each range merges on its own exactly as the whole would, and the ranges' runs laid end to end
// (their value offsets moved) are the bytes merge builds. One very hot account's runs late in a
// window are millions of records: their merge was the longest goroutine of every cut's Apply
// (~115-135 ms while the other plans took ~20 ms).
func mergePar(a, b *run, k keep) run {
	cuts := mergeSplits(a, b, min(mergeParMax, runtime.GOMAXPROCS(0), (a.n()+b.n())/(mergeParMin/2)))
	if len(cuts) <= 2 {
		return merge(a, b, k)
	}
	parts := make([]run, len(cuts)-1)
	var wg sync.WaitGroup
	for p := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, y := cuts[p], cuts[p+1]
			sa := run{rec: a.rec[x[0]*recSize : y[0]*recSize], vals: a.vals}
			sb := run{rec: b.rec[x[1]*recSize : y[1]*recSize], vals: b.vals}
			parts[p] = merge(&sa, &sb, k)
		}()
	}
	wg.Wait()
	n, nv := 0, 0
	for i := range parts {
		n += parts[i].n()
		nv += len(parts[i].vals)
	}
	if n == 0 {
		return run{}
	}
	out := run{rec: make([]byte, 0, n*recSize), vals: make([]byte, 0, nv)}
	for i := range parts {
		p := &parts[i]
		at, base := len(out.rec), uint32(len(out.vals))
		out.rec = append(out.rec, p.rec...)
		out.vals = append(out.vals, p.vals...)
		if base > 0 && len(p.vals) > 0 {
			for o := at; o < len(out.rec); o += recSize {
				if out.rec[o+34] > inlineMax {
					binary.LittleEndian.PutUint32(out.rec[o+36:], binary.LittleEndian.Uint32(out.rec[o+36:])+base)
				}
			}
		}
		out.top = max(out.top, p.top)
		out.multi += p.multi
	}
	bd := builder{out}
	return bd.finish()
}

// mergeSplits cuts a and b into up to w ranges at the same keys: pairs of record positions
// (in a, in b), the first {0, 0} and the last {a.n(), b.n()}; each cut is the first version of
// its key in both runs.
func mergeSplits(a, b *run, w int) [][2]int {
	big := a
	if b.n() > a.n() {
		big = b
	}
	cuts := [][2]int{{0, 0}}
	for p := 1; p < w; p++ {
		key := big.key(p * big.n() / w)
		c := [2]int{a.lower(&key), b.lower(&key)}
		if last := cuts[len(cuts)-1]; c[0] > last[0] || c[1] > last[1] {
			cuts = append(cuts, c)
		}
	}
	if last := cuts[len(cuts)-1]; last[0] < a.n() || last[1] < b.n() {
		cuts = append(cuts, [2]int{a.n(), b.n()})
	}
	return cuts
}

// hashParMin is the slot count from which slotKVs hashes on several goroutines.
var hashParMin = 1 << 14

// slotKV is a slot's keccak and its value.
type slotKV struct{ sh, v [32]byte }

// slotKVs is slots as (keccak(slot), value) in keccak order, and the value bytes over inlineMax.
// One very hot account writes ~10^5 slots in a cut: hashed on one goroutine they were the
// longest goroutine of every cut's decode (43-84 ms against ~5 ms), so a large set hashes on up
// to mergeParMax goroutines. The keys are distinct, so the order is the same however they hash.
func slotKVs(slots map[[32]byte][32]byte) ([]slotKV, int) {
	kvs := make([]slotKV, 0, len(slots))
	nv := 0
	for k, v := range slots {
		kvs = append(kvs, slotKV{k, v}) // sh holds the slot until hashed below
		if l := len(trim(&v)); l > inlineMax {
			nv += l
		}
	}
	hash := func(part []slotKV) {
		for i := range part {
			part[i].sh = base.Keccak(part[i].sh[:])
		}
	}
	if w := min(mergeParMax, runtime.GOMAXPROCS(0), len(kvs)/(hashParMin/2)); w > 1 {
		var wg sync.WaitGroup
		for p := range w {
			wg.Add(1)
			go func() {
				defer wg.Done()
				hash(kvs[p*len(kvs)/w : (p+1)*len(kvs)/w])
			}()
		}
		wg.Wait()
	} else {
		hash(kvs)
	}
	slices.SortFunc(kvs, slotKVCmp)
	return kvs, nv
}

// slotKVCmp orders by keccak (bytes.Compare of sh), the first 8 bytes as one number first.
func slotKVCmp(x, y slotKV) int {
	if a, b := binary.BigEndian.Uint64(x.sh[:8]), binary.BigEndian.Uint64(y.sh[:8]); a != b {
		return cmp.Compare(a, b)
	}
	return bytes.Compare(x.sh[8:], y.sh[8:])
}
