package trie

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unsafe"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// The trie top file: the snapshot's second section, the stored trie top of the base. It is a
// chunkfile (name = its hash) whose data part is (integers little-endian):
//
//	header 96 B: magic "FFTRIE01" | K u32 | account trie depth u32 | base block u64 |
//	        base content length u64 | accounts u64 | storage tries u64 | state root 32 |
//	        account form u8 (1: coreth 5 fields with the multicoin flag, 0: 4 fields) | zeros 15
//	account offsets: accounts x u64 (ordinal -> record offset in the base)
//	account trie:    bounds (16^d + 1) x u64 (account ordinals) | positions
//	storage tries, ascending by owner: owner 32 | depth u8 | bounds (16^d + 1) x u64 (base
//	        byte offsets) | positions
//	positions:       levels 0..d, level l has 16^l, each 34 B: reference 32 (zero after its
//	        length) | length u8 | kind u8
//
// It holds only base state (written right after BuildTop, before any overlay): with the base
// it restores the engine without hashing (Build's 1.67G hashes) or scanning the base. The
// bytes follow from the base alone, so every node writes the same file.
const (
	topMagic  = "FFTRIE01"
	topHeader = 96
	posSize   = 34
)

// WriteFile writes the top as a chunkfile <dir>/<hash hex>.top and returns its path, hash and
// size. The top must hold the base only (no overlay).
func (t *Top) WriteFile(dir string) (string, chunkfile.Hash, int64, error) {
	e := t.E
	if len(e.ov) > 0 || len(e.dirtyStor) > 0 || len(e.dirtyAcct) > 0 || t.Block != e.s.Block {
		return "", chunkfile.Hash{}, 0, errors.New("trie: the top file holds the base only; this top has an overlay")
	}
	if e.top != nil {
		return "", chunkfile.Hash{}, 0, errors.New("trie: this top was opened from its top file: write a built one")
	}
	tmp := filepath.Join(dir, "top.tmp")
	cw, err := chunkfile.Create(tmp)
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	bw := bufio.NewWriterSize(cw, 4<<20)
	owners := make([][32]byte, 0, len(e.stor))
	for ah := range e.stor {
		owners = append(owners, ah)
	}
	slices.SortFunc(owners, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	h := make([]byte, topHeader)
	copy(h, topMagic)
	binary.LittleEndian.PutUint32(h[8:], uint32(e.K))
	binary.LittleEndian.PutUint32(h[12:], uint32(e.acct.depth))
	binary.LittleEndian.PutUint64(h[16:], e.s.Block)
	binary.LittleEndian.PutUint64(h[24:], uint64(len(e.s.B)))
	binary.LittleEndian.PutUint64(h[32:], uint64(len(e.acctOff)))
	binary.LittleEndian.PutUint64(h[40:], uint64(len(owners)))
	copy(h[48:], t.Root[:])
	if e.FiveFields {
		h[80] = 1
	}
	bw.Write(h)
	var b [posSize]byte
	u64s := func(xs []uint64) {
		for _, x := range xs {
			binary.LittleEndian.PutUint64(b[:8], x)
			bw.Write(b[:8])
		}
	}
	trie := func(tr *Trie) {
		u64s(tr.bounds)
		for i := range tr.pos {
			p := &tr.pos[i]
			b = [posSize]byte{}
			copy(b[:], p.Ref[:p.N])
			b[32], b[33] = p.N, p.Kind
			bw.Write(b[:])
		}
	}
	u64s(e.acctOff)
	trie(e.acct)
	for _, ah := range owners {
		tr := e.stor[ah]
		bw.Write(ah[:])
		bw.WriteByte(tr.depth)
		trie(tr)
	}
	if err := bw.Flush(); err != nil {
		cw.Finish()
		os.Remove(tmp)
		return "", chunkfile.Hash{}, 0, err
	}
	hash, err := cw.Finish()
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	path := filepath.Join(dir, hex.EncodeToString(hash[:])+".top")
	if err := os.Rename(tmp, path); err != nil {
		return "", hash, 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", hash, 0, err
	}
	return path, hash, st.Size(), nil
}

var errTop = errors.New("trie: corrupt top file")

// OpenTop opens the top of b from its top file (checked against hash): OpenTopFile over the
// file opened alone (the file handle is closed again; the mapping stays).
func OpenTop(path string, hash chunkfile.Hash, b *base.Base, threads int) (*Top, error) {
	cf, err := chunkfile.Open(path, hash)
	if err != nil {
		return nil, err
	}
	defer cf.Close()
	return OpenTopFile(cf, path, b, threads)
}

// topFile is an opened top file, mapped read only: the account trie and every storage trie
// are read in place, and a trie is copied to the heap only where it changes (Trie.own).
type topFile struct {
	mm     []byte   // the mapping (header and data)
	m      []byte   // the data part
	tries  uint64   // offset of the first storage trie record
	end    uint64   // offset after the last
	bits   uint     // owner prefix bits of the bucket index
	bucket []uint64 // bucket b: offset of the first record whose owner prefix is >= b; + end
}

// recLen is the length of a storage trie record of depth d (owner, depth, bounds, positions).
func recLen(d uint8) uint64 {
	return 33 + 8*(uint64(1)<<(4*d)+1) + posSize*uint64(levelOff(int(d)+1))
}

func (f *topFile) prefix(ah *[32]byte) uint64 {
	if f.bits == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(ah[:8]) >> (64 - f.bits)
}

// find returns the offset of ah's storage trie record.
func (f *topFile) find(ah *[32]byte) (uint64, bool) {
	p := f.prefix(ah)
	for off, end := f.bucket[p], f.bucket[p+1]; off < end; off += recLen(f.m[off+32]) {
		switch bytes.Compare(f.m[off:off+32], ah[:]) {
		case 0:
			return off, true
		case 1:
			return 0, false
		}
	}
	return 0, false
}

// trie is the storage trie of the record at off, read in place.
func (f *topFile) trie(off uint64) Trie {
	d := f.m[off+32]
	nb := 8 * (uint64(1)<<(4*d) + 1)
	return Trie{depth: d, bb: f.m[off+33 : off+33+nb], pos: posAt(f.m[off+33+nb:], levelOff(int(d)+1)), ro: true}
}

// root is the root position of the record at off.
func (f *topFile) root(off uint64) *Pos {
	return &posAt(f.m[off+33+8*(uint64(1)<<(4*f.m[off+32])+1):], 1)[0]
}

// each calls fn for every storage trie record, ascending by owner.
func (f *topFile) each(fn func(ah [32]byte, off uint64)) {
	for off := f.tries; off < f.end; off += recLen(f.m[off+32]) {
		fn([32]byte(f.m[off:off+32]), off)
	}
}

var _ [posSize - unsafe.Sizeof(Pos{})]byte // Pos is the file's 34-byte position, byte aligned
var _ [unsafe.Sizeof(Pos{}) - posSize]byte

// posAt is the n positions at the start of b, in place.
func posAt(b []byte, n int) []Pos {
	if n == 0 {
		return nil
	}
	return unsafe.Slice((*Pos)(unsafe.Pointer(&b[:n*posSize][0])), n)
}

// trie returns ah's storage trie (nil: none): a touched one, else the top file's, read in place
// (not kept: a write goes through touch).
func (e *Engine) trie(ah *[32]byte) *Trie {
	if t, ok := e.stor[*ah]; ok || e.top == nil {
		return t
	}
	if off, ok := e.top.find(ah); ok {
		t := e.top.trie(off)
		return &t
	}
	return nil
}

// touch is trie for a write: a top file trie is kept from then on.
func (e *Engine) touch(ah *[32]byte) *Trie {
	if t, ok := e.stor[*ah]; ok || e.top == nil {
		return t
	}
	t := e.trie(ah)
	if t != nil {
		e.stor[*ah] = t
	}
	return t
}

// OpenTopFile opens the top of b from its top file at path, whose open handle is cf (the shared
// handle of its dist.Dir; OpenTopFile does not close it). The file must be complete. It is
// mapped and checked once from end to end (format, bounds, the account trie's root against the
// header's); afterwards its nodes are read in place and only changed positions take heap. It
// reads no base bytes beyond the base's index.
func OpenTopFile(cf *chunkfile.File, path string, b *base.Base, threads int) (*Top, error) {
	base := b.Snap()
	for i := cf.HeaderChunks(); i < cf.NumChunks(); i++ {
		if !cf.Present(i) {
			return nil, fmt.Errorf("trie: top file %s: %w", path, &chunkfile.AbsentError{Chunk: i})
		}
	}
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := fd.Stat()
	if err != nil {
		fd.Close()
		return nil, err
	}
	if st.Size() != cf.Size() {
		fd.Close()
		return nil, fmt.Errorf("trie: %s is %d B, its chunk file %d B", path, st.Size(), cf.Size())
	}
	mm, err := syscall.Mmap(int(fd.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	fd.Close()
	if err != nil {
		return nil, err
	}
	m := mm[st.Size()-cf.DataSize():]
	t, err := openTop(m, base, b.Check, threads)
	if err != nil {
		syscall.Munmap(mm)
		return nil, fmt.Errorf("trie: top file %s: %w", path, err)
	}
	t.E.top.mm = mm
	return t, nil
}

// Close unmaps the top file of a top opened from one (a no-op for a built top). The top must not
// be used after.
func (t *Top) Close() error {
	if err := t.E.closeL1(); err != nil {
		return err
	}
	f := t.E.top
	if f == nil || f.mm == nil {
		return nil
	}
	mm := f.mm
	f.mm, f.m = nil, nil
	return syscall.Munmap(mm)
}

func openTop(m []byte, base *era.Snap, check func(off, n uint64) error, threads int) (*Top, error) {
	if len(m) < topHeader {
		return nil, errTop
	}
	h := m[:topHeader]
	if string(h[:8]) != topMagic || h[80] > 1 || !allZero(h[81:]) {
		return nil, errTop
	}
	k, adepth := binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:])
	block, blen := binary.LittleEndian.Uint64(h[16:]), binary.LittleEndian.Uint64(h[24:])
	nacct, ntries := binary.LittleEndian.Uint64(h[32:]), binary.LittleEndian.Uint64(h[40:])
	if block != base.Block || blen != uint64(len(base.B)) {
		return nil, fmt.Errorf("trie: top file is of the base after block %d (%d B), the base is after block %d (%d B)", block, blen, base.Block, len(base.B))
	}
	stateEnd := base.StateEnd()
	if k == 0 || adepth > 7 || nacct > stateEnd/34 || ntries > nacct {
		return nil, errTop
	}
	e := &Engine{K: int(k), Threads: threads, FiveFields: h[80] == 1, s: base, check: check,
		stor: map[[32]byte]*Trie{}, ov: map[[32]byte]*ovAcct{},
		dirtyStor: map[[32]byte]map[uint32]subMask{}, dirtyAcct: map[uint32]subMask{}}
	off := uint64(topHeader)
	take := func(n uint64) ([]byte, bool) {
		if n > uint64(len(m))-off {
			return nil, false
		}
		off += n
		return m[off-n : off], true
	}
	u64sOK := func(b []byte, max uint64) bool {
		var prev uint64
		for i := 0; i < len(b); i += 8 {
			x := binary.LittleEndian.Uint64(b[i:])
			if x > max || x < prev {
				return false
			}
			prev = x
		}
		return true
	}
	posOK := func(ps []Pos) bool {
		for i := range ps {
			p := &ps[i]
			if p.N > 32 || p.Kind > kOther || !allZero(p.Ref[p.N:]) || (p.N == 0) != (p.Kind == kEmpty) {
				return false
			}
		}
		return true
	}
	// trie checks and returns the trie at off (bounds then positions) of depth d
	trie := func(d uint8, boundMax uint64) (Trie, bool) {
		bb, ok := take(8 * (uint64(1)<<(4*d) + 1))
		if !ok || !u64sOK(bb, boundMax) {
			return Trie{}, false
		}
		pb, ok := take(posSize * uint64(levelOff(int(d)+1)))
		if !ok {
			return Trie{}, false
		}
		ps := posAt(pb, levelOff(int(d)+1))
		return Trie{depth: d, bb: bb, pos: ps, ro: true}, posOK(ps)
	}
	ab, ok := take(8 * nacct)
	if !ok || !u64sOK(ab, stateEnd) {
		return nil, errTop
	}
	if uintptr(unsafe.Pointer(unsafe.SliceData(ab)))%8 == 0 && binary.NativeEndian.Uint16([]byte{1, 0}) == 1 {
		e.acctOff = unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.SliceData(ab))), nacct) // in place
	} else {
		e.acctOff = make([]uint64, nacct)
		for i := range e.acctOff {
			e.acctOff[i] = binary.LittleEndian.Uint64(ab[8*i:])
		}
	}
	at, ok := trie(uint8(adepth), nacct)
	if !ok || at.bound(0) != 0 || at.bound(at.cuts()) != nacct {
		return nil, errTop
	}
	e.acct = &at
	f := &topFile{m: m, tries: off}
	for f.bits < 40 && uint64(8)<<f.bits < ntries { // about 8 records a bucket
		f.bits++
	}
	f.bucket = make([]uint64, 1<<f.bits+1)
	next := uint64(0) // the next bucket whose start is unset
	var prev [32]byte
	for i := range ntries {
		rec := off
		hb, ok := take(33)
		if !ok {
			return nil, errTop
		}
		ah := [32]byte(hb[:32])
		d := hb[32]
		if d > 7 || i > 0 && bytes.Compare(ah[:], prev[:]) <= 0 {
			return nil, errTop
		}
		t, ok := trie(d, stateEnd)
		if !ok || t.bound(0) < era.HeaderLen {
			return nil, errTop
		}
		for p := f.prefix(&ah); next <= p; next++ {
			f.bucket[next] = rec
		}
		prev = ah
	}
	if off != uint64(len(m)) {
		return nil, errTop // trailing bytes
	}
	f.end = off
	for ; next < uint64(len(f.bucket)); next++ {
		f.bucket[next] = off
	}
	e.top = f
	var root [32]byte
	copy(root[:], h[48:80])
	if got := NewKeccaker().RootHash(e.acct.at(0)); got != root {
		return nil, fmt.Errorf("trie: top file root %x, its account trie hashes to %x", root, got)
	}
	return &Top{E: e, Block: block, Root: root}, nil
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
