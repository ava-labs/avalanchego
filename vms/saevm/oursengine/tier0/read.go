package tier0

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

var errCorrupt = errors.New("tier0: corrupt file")

// File is a tier0 data file opened for reading through the chunk layer: records are read with
// chunkfile ReadAt (a punched chunk reads as chunkfile.AbsentError). It reads format v6
// (compressed frames) and v5 (raw records, kept so cmd/tier0-rebuild and old directories
// read), and the raw view of a sealed tier0 (Sealed.File). Memory: the header, the tx count column (4 B per block) and per section the frame
// table (v6, ~1k frames) or the offset table (v5, 4 B per block); decoded frames live in the
// process-wide frame cache (SetFrameCache), dictionaries in its decoder LRU.
type File struct {
	cf           *chunkfile.File
	owned        bool // Open opened cf: Close closes it
	v5           bool
	sealed       bool // a sealed tier0's spools (raw records like v5, no chunk file)
	First        uint64
	FirstOrdinal uint64 // chain-wide ordinal of the file's first tx
	Blocks       int
	Txs          int
	cum          []uint32 // txs before each block, blocks+1 entries
	sec          [nsec]secInfo
}

// secInfo locates one section in the file.
type secInfo struct {
	stored int64 // bytes in the file (v6: prelude + frames; v5: offset table + records)
	raw    int64 // raw record bytes (v6: length prefixes included)
	// v5 and sealed: block i's record is off[i+1]-off[i] bytes at start + off[i], or for a
	// sealed tier0's bodies at pos[i] (inside the bodies store's records), read from ra
	// (nil: the chunk file)
	start int64
	off   []uint32
	pos   []int64
	ra    io.ReaderAt
	// v6: the prelude (dictionary or certificate table, zstd) and the frames; frame k holds
	// blocks fblock[k]..fblock[k+1]-1, its bytes are foff[k]..foff[k+1], fraw[k] raw bytes
	prelude, preludeLen int64
	preludeRaw          int
	fblock              []int32
	foff                []int64
	fraw                []int32
}

// Open opens the tier0 file at path and checks it against its file hash. It is for files only
// this handle reads; a node reads its tier0 files through the shared dist.Dir handle
// (OpenChunkFile), so a download filling the file is seen.
func Open(path string, want chunkfile.Hash) (*File, error) {
	cf, err := chunkfile.Open(path, want)
	if err != nil {
		return nil, err
	}
	f, err := parse(cf)
	if err != nil {
		cf.Close()
		return nil, err
	}
	f.owned = true
	return f, nil
}

// OpenChunkFile reads the tier0 file cf (the shared handle of its dist.Dir; Close leaves it
// open).
func OpenChunkFile(cf *chunkfile.File) (*File, error) { return parse(cf) }

func parse(cf *chunkfile.File) (*File, error) {
	m := make([]byte, 8)
	if cf.DataSize() < 8 {
		return nil, errCorrupt
	}
	if _, err := cf.ReadAt(m, 0); err != nil {
		return nil, err
	}
	switch string(m) {
	case magic:
		return parseV6(cf)
	case magic5:
		return parseV5(cf)
	}
	return nil, errCorrupt
}

// parseV6 reads the header, the trailer and the head (see the package comment).
func parseV6(cf *chunkfile.File) (*File, error) {
	size := cf.DataSize()
	if size < headerSize+trailerSize {
		return nil, errCorrupt
	}
	h := make([]byte, headerSize)
	t := make([]byte, trailerSize)
	if _, err := cf.ReadAt(h, 0); err != nil {
		return nil, err
	}
	if _, err := cf.ReadAt(t, size-trailerSize); err != nil {
		return nil, err
	}
	f := &File{cf: cf, First: binary.LittleEndian.Uint64(h[8:]), Blocks: int(binary.LittleEndian.Uint32(h[16:])), Txs: int(binary.LittleEndian.Uint32(h[20:])),
		FirstOrdinal: binary.LittleEndian.Uint64(h[24:])}
	headLen, headRaw := int64(binary.LittleEndian.Uint32(t)), int(binary.LittleEndian.Uint32(t[4:]))
	headAt := size - trailerSize - headLen
	// the raw head holds a tx count per block (>= 1 byte) and per section and frame a few
	// uvarints (frames <= blocks): it cannot be larger than ~200 B per block
	if f.Blocks == 0 || f.Blocks > 1<<26 || headAt < headerSize || headRaw < f.Blocks || headRaw > 200*f.Blocks+1024 {
		return nil, errCorrupt
	}
	hz := make([]byte, headLen)
	if _, err := cf.ReadAt(hz, headAt); err != nil {
		return nil, err
	}
	d, err := plainDecoder()
	if err != nil {
		return nil, err
	}
	head, err := decodeExact(d, hz, headRaw)
	if err != nil {
		return nil, err
	}
	c := &cursor{b: head}
	f.cum = make([]uint32, f.Blocks+1)
	for i := range f.Blocks {
		n := uint64(c.num())
		if c.err != nil || uint64(f.cum[i])+n > uint64(f.Txs) {
			return nil, errCorrupt
		}
		f.cum[i+1] = f.cum[i] + uint32(n)
	}
	if int(f.cum[f.Blocks]) != f.Txs {
		return nil, errCorrupt
	}
	at := int64(headerSize)
	for s := range nsec {
		x := &f.sec[s]
		x.prelude, x.preludeLen, x.preludeRaw = at, int64(c.num()), c.num()
		if (x.preludeLen == 0) != (x.preludeRaw == 0) {
			return nil, errCorrupt
		}
		at += x.preludeLen
		nf := c.num()
		if c.err != nil || nf == 0 || nf > f.Blocks {
			return nil, errCorrupt
		}
		x.fblock, x.foff, x.fraw = make([]int32, nf+1), make([]int64, nf+1), make([]int32, nf)
		x.foff[0] = at
		for k := range nf {
			nb, raw, n := c.num(), c.num(), c.num()
			// a frame holds >= 1 block and >= 1 raw byte per block (the length prefix)
			if c.err != nil || nb == 0 || raw < nb || raw > 1<<31 || n == 0 || int(x.fblock[k])+nb > f.Blocks {
				return nil, errCorrupt
			}
			x.fblock[k+1] = x.fblock[k] + int32(nb)
			x.fraw[k] = int32(raw)
			x.foff[k+1] = x.foff[k] + int64(n)
			x.raw += int64(raw)
		}
		if int(x.fblock[nf]) != f.Blocks {
			return nil, errCorrupt
		}
		at = x.foff[nf]
		x.stored = at - x.prelude
	}
	if c.err != nil || len(c.b) != 0 || at != headAt {
		return nil, errCorrupt
	}
	if f.sec[SecWrappers].preludeLen > 0 { // the certificate table must parse
		if _, err := f.certs(); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// parseV5 reads a format v5 file (raw records; see the package comment of v5 in git history:
// header 80 B, per section an offset table, bodies with the tx count column, then the records).
func parseV5(cf *chunkfile.File) (*File, error) {
	size := cf.DataSize()
	if size < headerSize5 {
		return nil, errCorrupt
	}
	h := make([]byte, headerSize5)
	if _, err := cf.ReadAt(h, 0); err != nil {
		return nil, err
	}
	f := &File{cf: cf, v5: true, First: binary.LittleEndian.Uint64(h[8:]), Blocks: int(binary.LittleEndian.Uint32(h[16:])), Txs: int(binary.LittleEndian.Uint32(h[20:])),
		FirstOrdinal: binary.LittleEndian.Uint64(h[24:])}
	if int64(nsec+1)*4*int64(f.Blocks+1) > size {
		return nil, errCorrupt
	}
	at := int64(headerSize5)
	for s := range nsec {
		if int64(binary.LittleEndian.Uint64(h[32+8*s:])) != at {
			return nil, errCorrupt
		}
		t := make([]byte, tableSize5(s, f.Blocks))
		if _, err := cf.ReadAt(t, at); err != nil {
			return nil, err
		}
		x := &f.sec[s]
		x.off = make([]uint32, f.Blocks+1)
		for i := range x.off {
			x.off[i] = binary.LittleEndian.Uint32(t[4*i:])
			if i > 0 && x.off[i] < x.off[i-1] || i == 0 && x.off[0] != 0 {
				return nil, errCorrupt
			}
		}
		if s == SecBodies {
			f.cum = make([]uint32, f.Blocks+1)
			for i := range f.cum {
				f.cum[i] = binary.LittleEndian.Uint32(t[4*(f.Blocks+1+i):])
				if i > 0 && f.cum[i] < f.cum[i-1] || i == 0 && f.cum[0] != 0 {
					return nil, errCorrupt
				}
			}
			if int(f.cum[f.Blocks]) != f.Txs {
				return nil, errCorrupt
			}
		}
		x.start = at + int64(len(t))
		x.raw = int64(x.off[f.Blocks])
		x.stored = int64(len(t)) + x.raw
		at = x.start + x.raw
	}
	if at != size {
		return nil, errCorrupt
	}
	return f, nil
}

// Locate maps a chain-wide tx ordinal to its block (position in the file) and its position in
// the block; ok is false when the ordinal is not in this file.
func (f *File) Locate(ordinal uint64) (block, pos int, ok bool) {
	if ordinal < f.FirstOrdinal || ordinal-f.FirstOrdinal >= uint64(f.Txs) {
		return 0, 0, false
	}
	o := uint32(ordinal - f.FirstOrdinal)
	// the last block whose first tx is at or before o (empty blocks share a cum value)
	block = sort.Search(f.Blocks, func(i int) bool { return f.cum[i+1] > o })
	return block, int(o - f.cum[block]), true
}

// Ordinal is the chain-wide ordinal of block i's first tx.
func (f *File) Ordinal(i int) uint64 { return f.FirstOrdinal + uint64(f.cum[i]) }

// Close closes the file and drops its frames and decoders from the cache.
func (f *File) Close() error {
	cache.drop(f)
	if f.owned {
		return f.cf.Close()
	}
	return nil
}

// Version is the file's format version (5 or 6; 0 for a sealed tier0's view).
func (f *File) Version() int {
	switch {
	case f.sealed:
		return 0
	case f.v5:
		return 5
	}
	return 6
}

// SectionSize is the bytes section s takes in the file (v6: its dictionary or certificate
// table and its frames; v5: its offset table and records; sealed: its records).
func (f *File) SectionSize(s int) int { return int(f.sec[s].stored) }

// RawSize is section s's raw record bytes (v6: with the per-record length prefixes).
func (f *File) RawSize(s int) int { return int(f.sec[s].raw) }

// HeadSize is the bytes outside the sections (v6: header, head and trailer; v5: header).
func (f *File) HeadSize() int {
	if f.sealed {
		return 0
	}
	n := f.cf.DataSize()
	for s := range nsec {
		n -= f.sec[s].stored
	}
	return int(n)
}

// Frames is the number of frames of section s (v6; 0 for v5 and sealed).
func (f *File) Frames(s int) int {
	if f.v5 || f.sealed {
		return 0
	}
	return len(f.sec[s].fraw)
}

// Record is block i's record in section s (i is the position in the file, 0-based), raw as
// written (wrappers in the v5 form: uvarint len(pre) | pre | post). The slice is the
// caller's.
func (f *File) Record(s, i int) ([]byte, error) {
	if i < 0 || i >= f.Blocks {
		return nil, fmt.Errorf("tier0: block position %d of %d", i, f.Blocks)
	}
	if f.v5 || f.sealed {
		x := &f.sec[s]
		lo, hi := x.off[i], x.off[i+1]
		b := make([]byte, hi-lo)
		if len(b) == 0 {
			return b, nil
		}
		at := x.start + int64(lo)
		if x.pos != nil {
			at = x.pos[i]
		}
		var err error
		if x.ra != nil {
			_, err = x.ra.ReadAt(b, at)
		} else {
			_, err = f.cf.ReadAt(b, at)
		}
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	k := f.frameOf(s, i)
	key := frameKey{f, s, k}
	fr := cache.get(key)
	if fr == nil {
		var err error
		if fr, err = f.decodeFrame(s, k); err != nil {
			return nil, err
		}
		cache.put(key, fr)
	}
	return f.fromFrame(s, i, k, fr)
}

// fromFrame is block i's record out of its decoded frame k, as Record returns it.
func (f *File) fromFrame(s, i, k int, fr *frame) ([]byte, error) {
	j := i - int(f.sec[s].fblock[k])
	r := fr.data[fr.off[2*j]:fr.off[2*j+1]]
	if s == SecWrappers {
		certs, err := f.certs()
		if err != nil {
			return nil, err
		}
		return certJoin(r, certs)
	}
	return bytes.Clone(r), nil
}

// frameOf is the frame of section s holding block i.
func (f *File) frameOf(s, i int) int {
	fb := f.sec[s].fblock
	return sort.Search(len(fb)-1, func(k int) bool { return int(fb[k+1]) > i })
}

// decodeFrame reads and decodes frame k of section s and indexes its records.
func (f *File) decodeFrame(s, k int) (*frame, error) {
	x := &f.sec[s]
	z := make([]byte, x.foff[k+1]-x.foff[k])
	if _, err := f.cf.ReadAt(z, x.foff[k]); err != nil {
		return nil, err
	}
	d, err := f.decoderFor(s)
	if err != nil {
		return nil, err
	}
	data, err := decodeExact(d, z, int(x.fraw[k]))
	if err != nil {
		return nil, fmt.Errorf("section %s frame %d: %w", SectionNames[s], k, err)
	}
	n := int(x.fblock[k+1] - x.fblock[k])
	fr := &frame{data: data, off: make([]int, 0, 2*n)}
	c := &cursor{b: data}
	for range n {
		l := c.uvarint()
		at := len(data) - len(c.b)
		fr.off = append(fr.off, at, at+l)
		c.take(l)
	}
	if c.err != nil || len(c.b) != 0 {
		return nil, fmt.Errorf("section %s frame %d: %w", SectionNames[s], k, errCorrupt)
	}
	return fr, nil
}

// prelude reads and decodes section s's prelude (dictionary or certificate table).
func (f *File) prelude(s int) ([]byte, error) {
	x := &f.sec[s]
	z := make([]byte, x.preludeLen)
	if _, err := f.cf.ReadAt(z, x.prelude); err != nil {
		return nil, err
	}
	d, err := plainDecoder()
	if err != nil {
		return nil, err
	}
	return decodeExact(d, z, x.preludeRaw)
}

// certs is the file's certificate table, cached like a frame (key frame -1).
func (f *File) certs() ([][]byte, error) {
	if f.sec[SecWrappers].preludeLen == 0 {
		return nil, nil
	}
	key := frameKey{f, SecWrappers, -1}
	if fr := cache.get(key); fr != nil {
		return fr.certs, nil
	}
	t, err := f.prelude(SecWrappers)
	if err != nil {
		return nil, err
	}
	cs, err := parseCerts(t)
	if err != nil {
		return nil, err
	}
	cache.put(key, &frame{data: t, certs: cs})
	return cs, nil
}

// Seq reads a file's records for one goroutine walking blocks in order (the fold, the trie,
// the store): it keeps the last decoded frame of each section itself and leaves the shared
// frame cache alone, so a whole-file pass neither evicts the frames RPC reads nor takes the
// cache lock per record. Random order works too (one frame decode per frame change).
type Seq struct {
	f    *File
	last [nsec]struct {
		k  int
		fr *frame
	}
}

// Seq returns a sequential reader of f.
func (f *File) Seq() *Seq {
	q := &Seq{f: f}
	for s := range q.last {
		q.last[s].k = -1
	}
	return q
}

// Record is File.Record through the reader's own frames.
func (q *Seq) Record(s, i int) ([]byte, error) {
	f := q.f
	if f.v5 || f.sealed || i < 0 || i >= f.Blocks {
		return f.Record(s, i)
	}
	k := f.frameOf(s, i)
	l := &q.last[s]
	if l.k != k {
		fr, err := f.decodeFrame(s, k)
		if err != nil {
			return nil, err
		}
		l.k, l.fr = k, fr
	}
	return f.fromFrame(s, i, k, l.fr)
}

// Body is block i's body.
func (f *File) Body(i int) ([]byte, error) { return f.Record(SecBodies, i) }

// Body is File.Body through the reader.
func (q *Seq) Body(i int) ([]byte, error) { return q.Record(SecBodies, i) }

// Wrapper is block i's wrapper, nil when the block has none.
func (f *File) Wrapper(i int) (*Wrapper, error) { return wrapper(f.Record(SecWrappers, i)) }

// Wrapper is File.Wrapper through the reader.
func (q *Seq) Wrapper(i int) (*Wrapper, error) { return wrapper(q.Record(SecWrappers, i)) }

func wrapper(r []byte, err error) (*Wrapper, error) {
	if err != nil {
		return nil, err
	}
	return parseWrapper(r)
}

// Container is block i's container bytes: wrapper pre | body | wrapper post, or the body when
// the block has no wrapper.
func (f *File) Container(i int) ([]byte, error) { return container(f.Body(i))(f.Wrapper(i)) }

// Container is File.Container through the reader.
func (q *Seq) Container(i int) ([]byte, error) { return container(q.Body(i))(q.Wrapper(i)) }

func container(body []byte, err error) func(*Wrapper, error) ([]byte, error) {
	return func(w *Wrapper, werr error) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		if werr != nil || w == nil {
			return body, werr
		}
		out := make([]byte, 0, len(w.Pre)+len(body)+len(w.Post))
		return append(append(append(out, w.Pre...), body...), w.Post...), nil
	}
}

// Receipts is block i's receipts in consensus encoding (bloom rebuilt from the logs).
func (f *File) Receipts(i int) ([][]byte, error) { return receipts(f.Record(SecReceipts, i)) }

// Receipts is File.Receipts through the reader.
func (q *Seq) Receipts(i int) ([][]byte, error) { return receipts(q.Record(SecReceipts, i)) }

func receipts(r []byte, err error) ([][]byte, error) {
	if err != nil {
		return nil, err
	}
	rs, err := readList(r)
	for j := range rs {
		if err == nil {
			rs[j], err = consensusReceipt(rs[j])
		}
	}
	return rs, err
}

// Outputs is block i's tx outputs.
func (f *File) Outputs(i int) ([][]byte, error) { return listOf(f.Record(SecOutputs, i)) }

// Outputs is File.Outputs through the reader.
func (q *Seq) Outputs(i int) ([][]byte, error) { return listOf(q.Record(SecOutputs, i)) }

// Codes is the code of every code hash block i's writes set, sorted by code hash.
func (f *File) Codes(i int) ([][]byte, error) { return listOf(f.Record(SecCodes, i)) }

// Codes is File.Codes through the reader.
func (q *Seq) Codes(i int) ([][]byte, error) { return listOf(q.Record(SecCodes, i)) }

// Writes is block i's write sets: pre-tx ops, one group per tx, end-of-block ops.
func (f *File) Writes(i int) ([][]Write, error) { return writes(f.Record(SecWrites, i)) }

// Writes is File.Writes through the reader.
func (q *Seq) Writes(i int) ([][]Write, error) { return writes(q.Record(SecWrites, i)) }

func listOf(r []byte, err error) ([][]byte, error) {
	if err != nil {
		return nil, err
	}
	return readList(r)
}

func writes(r []byte, err error) ([][]Write, error) {
	if err != nil {
		return nil, err
	}
	return decodeWrites(r)
}

type cursor struct {
	b   []byte
	err error
}

func (c *cursor) uvarint() int {
	if c.err != nil {
		return 0
	}
	v, n := binary.Uvarint(c.b)
	if n <= 0 || v > uint64(len(c.b)-n) { // every count or length is bounded by the bytes left
		c.err = errCorrupt
		return 0
	}
	c.b = c.b[n:]
	return int(v)
}

// num reads a uvarint that is a count or size of something elsewhere (not of the bytes that
// follow), at most 2^40.
func (c *cursor) num() int {
	if c.err != nil {
		return 0
	}
	v, n := binary.Uvarint(c.b)
	if n <= 0 || v > 1<<40 {
		c.err = errCorrupt
		return 0
	}
	c.b = c.b[n:]
	return int(v)
}

func (c *cursor) take(n int) []byte {
	if c.err != nil || n > len(c.b) {
		c.err = errCorrupt
		return nil
	}
	x := c.b[:n:n]
	c.b = c.b[n:]
	return x
}

func readList(b []byte) ([][]byte, error) {
	c := &cursor{b: b}
	n := c.uvarint()
	out := make([][]byte, 0, n)
	for range n {
		out = append(out, c.take(c.uvarint()))
		if c.err != nil {
			return nil, c.err
		}
	}
	if c.err != nil || len(c.b) != 0 {
		return nil, errCorrupt
	}
	return out, nil
}

// writesScratch holds decodeWrites' working buffers between calls.
var writesScratch = sync.Pool{New: func() any { return new(writesBuf) }}

type writesBuf struct {
	ws   []Write
	ends []int
}

// decodeWrites decodes a write sets record, refusing a non-canonical one. The writes are
// decoded into a reused buffer and copied once into one array of the exact size, which the
// groups slice (capped: an append to a group never reaches the next one); growing each group
// by append allocated about twice the writes, the largest allocation site of the read side.
func decodeWrites(r []byte) ([][]Write, error) {
	buf := writesScratch.Get().(*writesBuf)
	defer writesScratch.Put(buf)
	ws, ends := buf.ws[:0], buf.ends[:0]
	defer func() { buf.ws, buf.ends = ws[:0], ends[:0] }()
	c := &cursor{b: r}
	ng := c.uvarint()
	out := make([][]Write, 0, ng)
	for range ng {
		na := c.uvarint()
		for a := range na {
			var addr [20]byte
			copy(addr[:], c.take(20))
			n := c.uvarint()
			if c.err != nil || n == 0 || a > 0 && bytes.Compare(addr[:], ws[len(ws)-1].Addr[:]) <= 0 {
				return nil, errCorrupt
			}
			for j := range n {
				w := Write{Addr: addr}
				if k := c.take(1); k != nil {
					w.Kind = k[0]
				}
				if w.Kind > KindMulti {
					return nil, fmt.Errorf("tier0: write kind %d", w.Kind)
				}
				if w.Kind == KindSlot {
					copy(w.Slot[:], c.take(32))
				}
				c.value(&w.Old)
				c.value(&w.New)
				if c.err == nil && !validWrite(&w) {
					return nil, errCorrupt
				}
				if c.err != nil || j > 0 && cmpWrite(&ws[len(ws)-1], &w) >= 0 {
					return nil, errCorrupt // canonical: keys strictly ascending
				}
				ws = append(ws, w)
			}
		}
		if c.err != nil {
			return nil, c.err
		}
		ends = append(ends, len(ws))
	}
	if c.err != nil || len(c.b) != 0 {
		return nil, errCorrupt
	}
	all := make([]Write, len(ws))
	copy(all, ws)
	start := 0
	for _, end := range ends {
		var g []Write // nil for a group without writes, as before
		if end > start {
			g = all[start:end:end]
		}
		out = append(out, g)
		start = end
	}
	return out, nil
}

// value reads a value written by appendValue (canonical: no leading zero byte).
func (c *cursor) value(v *[32]byte) {
	l := c.take(1)
	if l == nil || l[0] > 32 {
		c.err = errCorrupt
		return
	}
	b := c.take(int(l[0]))
	if len(b) > 0 && b[0] == 0 {
		c.err = errCorrupt
		return
	}
	copy(v[32-len(b):], b)
}

// Facts is one block's record of the .facts sidecar: what the index step needs. Every set is
// sorted and without duplicates; tx hashes are in tx order, so a tx's locator is
// (Number, position).
//
// Sidecar layout: magic "FFT0FCT\x04" | first block u64 LE | blocks u32 LE | first tx ordinal
// u64 LE | per block:
// uvarint ntx + 32 B hashes, then four sets each as uvarint count + fixed-size keys:
// accounts with field writes (20 B), written slots (address 20 | slot 32), log addresses (20 B),
// log topics (32 B); then the header hash (32 B) and a fifth set, the accounts with a
// code-hash write (20 B), and a sixth, the accounts with a nonce write (20 B). Magic
// "FFT0FCT\x03" sidecars end after the fifth set (NonceAccts read as nil), "FFT0FCT\x02" ones
// after the four sets (Hash and CodeAccts read as zero too); FactsHeader.Version tells which.
type Facts struct {
	Number    uint64
	TxHashes  [][32]byte
	Accts     [][20]byte
	Slots     [][52]byte
	LogAddrs  [][20]byte
	LogTopics [][32]byte
	// Hash is the block's header hash and CodeAccts the accounts with a code-hash write in the
	// block (creation, deletion, EIP-161 removal), sorted and unique: the index's block section
	// and lifecycle rows. Zero when the sidecar predates them (piece.DeriveFacts fills them
	// from the tier0 file).
	Hash      [32]byte
	CodeAccts [][20]byte
	// NonceAccts are the accounts with a nonce write in the block, sorted and unique: the
	// index's nonce rows. Nil when the sidecar predates them (version < 4).
	NonceAccts [][20]byte
}

// FactsHeader is the .facts sidecar header.
type FactsHeader struct {
	First        uint64 // first block
	Blocks       int
	FirstOrdinal uint64 // chain-wide ordinal of the first tx
	Version      int    // sidecar format: 2, 3 or 4 (see Facts)
}

// factsVersion is the format version of a sidecar magic, 0 when it is none.
func factsVersion(m []byte) int {
	switch string(m) {
	case factsMagic:
		return 4
	case factsMagic3:
		return 3
	case factsMagic2:
		return 2
	}
	return 0
}

// ReadFactsHeader reads the header of a .facts sidecar.
func ReadFactsHeader(path string) (FactsHeader, error) {
	fl, err := os.Open(path)
	if err != nil {
		return FactsHeader{}, err
	}
	defer fl.Close()
	h := make([]byte, factsHead)
	if _, err := io.ReadFull(fl, h); err != nil || factsVersion(h[:8]) == 0 {
		return FactsHeader{}, errCorrupt
	}
	return FactsHeader{binary.LittleEndian.Uint64(h[8:]), int(binary.LittleEndian.Uint32(h[16:])), binary.LittleEndian.Uint64(h[20:]), factsVersion(h[:8])}, nil
}

// EachFacts streams a .facts sidecar, one block at a time.
func EachFacts(path string, f func(*Facts) error) error {
	fl, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fl.Close()
	st, err := fl.Stat()
	if err != nil {
		return err
	}
	r := &factsReader{r: bufio.NewReaderSize(fl, 1<<20), left: st.Size()}
	h := r.take(factsHead)
	if r.err != nil || factsVersion(h[:8]) == 0 {
		return errCorrupt
	}
	v := factsVersion(h[:8])
	first, n := binary.LittleEndian.Uint64(h[8:]), int(binary.LittleEndian.Uint32(h[16:]))
	for i := range n {
		x := &Facts{Number: first + uint64(i)}
		x.TxHashes = readKeys(r, 32, k32)
		x.Accts = readKeys(r, 20, k20)
		x.Slots = readKeys(r, 52, k52)
		x.LogAddrs = readKeys(r, 20, k20)
		x.LogTopics = readKeys(r, 32, k32)
		if v >= 3 {
			if hb := r.take(32); hb != nil {
				x.Hash = [32]byte(hb)
			}
			x.CodeAccts = readKeys(r, 20, k20)
		}
		if v >= 4 {
			x.NonceAccts = readKeys(r, 20, k20)
		}
		if r.err != nil {
			return r.err
		}
		if err := f(x); err != nil {
			return err
		}
	}
	if r.left != 0 {
		return errCorrupt
	}
	return nil
}

// ReadFacts reads a whole .facts sidecar (one tier0's index input).
func ReadFacts(path string) ([]Facts, error) {
	var out []Facts
	err := EachFacts(path, func(f *Facts) error { out = append(out, *f); return nil })
	return out, err
}

// factsReader reads a stream whose remaining length is known, so no count can claim more
// bytes than are left.
type factsReader struct {
	r    *bufio.Reader
	left int64
	err  error
}

func (r *factsReader) take(n int64) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > r.left {
		r.err = errCorrupt
		return nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		r.err = err
		return nil
	}
	r.left -= n
	return b
}

func readKeys[K any](r *factsReader, size int, conv func([]byte) K) []K {
	if r.err != nil {
		return nil
	}
	v, err := binary.ReadUvarint(r.r)
	if err != nil || v > uint64(r.left) {
		r.err = errCorrupt
		return nil
	}
	r.left -= int64(len(binary.AppendUvarint(nil, v)))
	raw := r.take(int64(v) * int64(size))
	if r.err != nil {
		return nil
	}
	out := make([]K, v)
	for i := range out {
		out[i] = conv(raw[i*size:])
	}
	return out
}

func k20(b []byte) [20]byte { return [20]byte(b) }
func k32(b []byte) [32]byte { return [32]byte(b) }
func k52(b []byte) [52]byte { return [52]byte(b) }
