package tier0

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/internal/zdict"
)

// Compression parameters of format v6, chosen from the measurement table in the faster firewood
// devlog of 2026-10-22 (compress executor, `cmd/t0zstd` over real tier0 files). They are part
// of the format: changing any of them, or the pinned github.com/klauspost/compress version
// (its encoder and its zstd.BuildDict make the bytes), changes file hashes and needs a new
// format version.
const (
	// frameRaw: a frame closes after the block record that brings its raw bytes (length
	// prefixes included) to frameRaw or more, so one block's record costs one frame to decode.
	frameRaw = 128 << 10
	// zLevel is the zstd level of every frame, dictionary and head: klauspost's "better"
	// (zstd level 6 class), ~10 B/tx under level 3 for encode CPU only.
	zLevel = zstd.SpeedBetterCompression
	// trainMax bounds the bytes a section's dictionary is trained on (an even-stride subset
	// of its records, zdict.Subset): training memory and time grow with the input.
	trainMax = 16 << 20
	// encWorkers bounds the frames encoded side by side at a cut (each encoder holds a few
	// MB of tables).
	encWorkers = 8
)

// dictSize is each section's trained dictionary size, 0 = no dictionary.
var dictSize = [nsec]int{
	SecBodies:   128 << 10,
	SecWrappers: 0, // the certificate table does what a dictionary would (see certSplit)
	SecReceipts: 256 << 10,
	SecWrites:   256 << 10,
	SecOutputs:  0,
	SecCodes:    0,
}

func encoderOpts(dict []byte) []zstd.EOption {
	// CRC on: 4 B per frame (~0.1 B/tx) so a frame damaged at rest fails its read instead of
	// returning other bytes (the chunk hashes are checked on fetch and on suspicion only)
	o := []zstd.EOption{zstd.WithEncoderLevel(zLevel), zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true)}
	if dict != nil {
		o = append(o, zstd.WithEncoderDict(dict))
	}
	return o
}

// encSection is one section of a v6 file, encoded.
type encSection struct {
	prelude    []byte   // stored dictionary or certificate table, zstd-compressed; nil: none
	preludeRaw int      // its raw bytes
	frames     [][]byte // compressed frames
	blocks     []int    // blocks in each frame
	raws       []int    // raw bytes of each frame
}

// frameSplit returns, for the records (one per block), the blocks of each frame: a frame
// closes after the record that brings it to frameRaw raw bytes or more (uvarint length
// prefixes counted).
func frameSplit(recs [][]byte) (blocks, raws []int) {
	n, raw := 0, 0
	for _, r := range recs {
		n++
		raw += uvarintLen(len(r)) + len(r)
		if raw >= frameRaw {
			blocks, raws = append(blocks, n), append(raws, raw)
			n, raw = 0, 0
		}
	}
	if n > 0 {
		blocks, raws = append(blocks, n), append(raws, raw)
	}
	return blocks, raws
}

func uvarintLen(v int) int { return len(binary.AppendUvarint(nil, uint64(v))) }

// encodeSection encodes section s from its raw records (one per block, in block order). The
// result depends only on the records: dictionary training is deterministic and every frame is
// one EncodeAll call (single-threaded, independent of the encoder's earlier calls), so
// frames are encoded in parallel without changing a byte.
func encodeSection(s int, recs [][]byte) (*encSection, error) {
	e := &encSection{}
	var dict []byte
	switch {
	case s == SecWrappers:
		var table []byte
		var err error
		if recs, table, err = certSplit(recs); err != nil {
			return nil, err
		}
		if len(table) > 0 {
			e.prelude, e.preludeRaw = plainEncoder().EncodeAll(table, nil), len(table)
		}
	case dictSize[s] > 0:
		var samples [][]byte
		for _, r := range recs {
			if len(r) > 0 {
				samples = append(samples, r)
			}
		}
		// No dictionary when the records repeat nothing (zdict.Build fails the same way on
		// every machine): the frames are then encoded without one.
		if d, err := zdict.Build(zdict.Subset(samples, trainMax), dictSize[s], zLevel, uint32(s+1)); err == nil {
			dict = d
			e.prelude, e.preludeRaw = plainEncoder().EncodeAll(d, nil), len(d)
		}
	}
	e.blocks, e.raws = frameSplit(recs)
	workers := min(runtime.GOMAXPROCS(0), encWorkers, len(e.blocks))
	// EncodeAll is one goroutine per call whatever the concurrency: concurrency only lets
	// calls run side by side
	enc, err := zstd.NewWriter(nil, append(encoderOpts(dict), zstd.WithEncoderConcurrency(workers))...)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	e.frames = make([][]byte, len(e.blocks))
	starts := make([]int, len(e.blocks))
	for k := 1; k < len(starts); k++ {
		starts[k] = starts[k-1] + e.blocks[k-1]
	}
	var wg sync.WaitGroup
	next := atomic.Int64{}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				k := int(next.Add(1) - 1)
				if k >= len(e.frames) {
					return
				}
				raw := make([]byte, 0, e.raws[k])
				for _, r := range recs[starts[k] : starts[k]+e.blocks[k]] {
					raw = binary.AppendUvarint(raw, uint64(len(r)))
					raw = append(raw, r...)
				}
				e.frames[k] = enc.EncodeAll(raw, nil)
			}
		}()
	}
	wg.Wait()
	return e, nil
}

var (
	plainOnce   sync.Once
	plainEnc    *zstd.Encoder
	plainDec    *zstd.Decoder
	plainDecErr error
)

// plainEncoder encodes without a dictionary (the head, preludes, sections without one).
func plainEncoder() *zstd.Encoder {
	plainOnce.Do(initPlain)
	return plainEnc
}

func plainDecoder() (*zstd.Decoder, error) {
	plainOnce.Do(initPlain)
	return plainDec, plainDecErr
}

func initPlain() {
	var err error
	if plainEnc, err = zstd.NewWriter(nil, encoderOpts(nil)...); err != nil {
		panic(err) // fixed options: cannot fail
	}
	plainDec, plainDecErr = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecodeAllCapLimit(true))
}

// decodeExact decodes one zstd frame that must hold exactly n bytes (the head says how many):
// the output buffer is capped at n, so a corrupt frame cannot make the decoder allocate more.
func decodeExact(d *zstd.Decoder, src []byte, n int) ([]byte, error) {
	out, err := d.DecodeAll(src, make([]byte, 0, n))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCorrupt, err)
	}
	if len(out) != n {
		return nil, errCorrupt
	}
	return out, nil
}

// Proposervm wrappers keep the block's certificate inside pre: pre = codec version u16 |
// type id u32 (0 statelessBlock, 2 statelessGraniteBlock) | parent ID 32 | timestamp u64 |
// P-Chain height u64 | certificate length u32 | certificate | inner block length u32 (all
// big-endian). A file's blocks come from a few hundred proposers, so the certificates go into
// a per-file table and the record keeps an index.
const (
	certAt  = 2 + 4 + 32 + 8 + 8 // offset of the certificate length in pre
	certMin = certAt + 4 + 4     // pre without the certificate bytes
)

// certOf returns the certificate in pre when pre is a signed proposervm block prefix whose
// certificate can be taken out (the only form the stored record allows taking out).
func certOf(pre []byte) []byte {
	if len(pre) <= certMin || pre[0] != 0 || pre[1] != 0 || pre[2] != 0 || pre[3] != 0 || pre[4] != 0 || pre[5] != 0 && pre[5] != 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint32(pre[certAt:]))
	if n == 0 || len(pre) != certMin+n {
		return nil
	}
	return pre[certAt+4 : certAt+4+n]
}

// certSplit turns wrapper records (as in v5: empty, or uvarint len(pre) | pre | post) into the
// stored v6 form and the certificate table. Stored record: empty when the block has no
// wrapper; else uvarint ref | uvarint len(pre') | pre' | post, where ref = 0 keeps pre whole
// (pre' = pre) and ref = k >= 1 takes certificate k-1 of the table out of pre (pre' = pre
// without the certificate length and bytes). Table: per certificate uvarint length | bytes, in
// first-use order, each once. The writer takes out every certificate certOf finds.
func certSplit(recs [][]byte) (out [][]byte, table []byte, err error) {
	idx := map[string]int{}
	out = make([][]byte, len(recs))
	for i, r := range recs {
		w, err := parseWrapper(r)
		if err != nil {
			return nil, nil, err
		}
		if w == nil {
			continue
		}
		pre, ref := w.Pre, 0
		if c := certOf(pre); c != nil {
			k, ok := idx[string(c)]
			if !ok {
				k = len(idx)
				idx[string(c)] = k
				table = binary.AppendUvarint(table, uint64(len(c)))
				table = append(table, c...)
			}
			ref = k + 1
			pre = append(pre[:certAt:certAt], pre[certAt+4+len(c):]...)
		}
		x := binary.AppendUvarint(nil, uint64(ref))
		x = binary.AppendUvarint(x, uint64(len(pre)))
		out[i] = append(append(x, pre...), w.Post...)
	}
	return out, table, nil
}

// parseCerts reads a certificate table.
func parseCerts(table []byte) ([][]byte, error) {
	var out [][]byte
	c := &cursor{b: table}
	for len(c.b) > 0 && c.err == nil {
		n := c.uvarint()
		if n == 0 {
			return nil, errCorrupt
		}
		out = append(out, c.take(n))
	}
	return out, c.err
}

// certJoin turns a stored wrappers record back into the v5 form (uvarint len(pre) | pre |
// post), refusing a record the writer would not have written.
func certJoin(r []byte, certs [][]byte) ([]byte, error) {
	if len(r) == 0 {
		return nil, nil
	}
	c := &cursor{b: r}
	ref := c.num() // an index into the table, not a length: not bounded by the record
	pre := c.take(c.uvarint())
	if c.err != nil {
		return nil, c.err
	}
	if ref == 0 {
		if certOf(pre) != nil {
			return nil, errCorrupt // the writer takes out every certificate it can
		}
	} else {
		if ref > len(certs) || len(pre) < certAt+4 {
			return nil, errCorrupt
		}
		cert := certs[ref-1]
		full := make([]byte, 0, len(pre)+4+len(cert))
		full = append(full, pre[:certAt]...)
		full = binary.BigEndian.AppendUint32(full, uint32(len(cert)))
		full = append(append(full, cert...), pre[certAt:]...)
		if certOf(full) == nil {
			return nil, errCorrupt
		}
		pre = full
	}
	x := binary.AppendUvarint(nil, uint64(len(pre)))
	return append(append(x, pre...), c.b...), nil
}

// The frame cache holds decoded frames of every open File, least recently used out, bounded
// by bytes: random block access (RPC) decodes a frame once and serves its neighbours from
// memory. Sequential readers use Seq instead and leave the cache alone.
type frameKey struct {
	f    *File
	s, k int
}

type frame struct {
	data  []byte
	off   []int    // record j of the frame is data[off[2j]:off[2j+1]]
	certs [][]byte // the certificate table's entry (frame -1 of the wrappers): its certificates
}

type frameCache struct {
	mu      sync.Mutex
	max     int
	size    int
	ll      *list.List // front = most recent; values *cacheEntry
	m       map[frameKey]*list.Element
	hits    uint64
	misses  uint64
	decoder map[decKey]*list.Element // dictionary decoders, least recently used out
	decLL   *list.List
}

type cacheEntry struct {
	k  frameKey
	fr *frame
}

type decKey struct {
	f *File
	s int
}

type decEntry struct {
	k decKey
	d *zstd.Decoder
}

// maxDecoders bounds the dictionary decoders kept (each holds its parsed dictionary and up to
// a frame of buffers, ~0.5-1 MB).
const maxDecoders = 64

var cache = &frameCache{max: 64 << 20, ll: list.New(), m: map[frameKey]*list.Element{}, decoder: map[decKey]*list.Element{}, decLL: list.New()}

// SetFrameCache sets the frame cache's size in bytes (default 64 MiB; 0 disables it).
func SetFrameCache(bytes int) {
	cache.mu.Lock()
	cache.max = bytes
	cache.evict()
	cache.mu.Unlock()
}

// CacheStats returns the frame cache's hits and misses since the process started.
func CacheStats() (hits, misses uint64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.hits, cache.misses
}

func (c *frameCache) get(k frameKey) *frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[k]; ok {
		c.ll.MoveToFront(e)
		c.hits++
		return e.Value.(*cacheEntry).fr
	}
	c.misses++
	return nil
}

func (c *frameCache) put(k frameKey, fr *frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; ok || len(fr.data) > c.max {
		return
	}
	c.m[k] = c.ll.PushFront(&cacheEntry{k, fr})
	c.size += len(fr.data)
	c.evict()
}

func (c *frameCache) evict() {
	for c.size > c.max {
		e := c.ll.Back()
		x := e.Value.(*cacheEntry)
		c.ll.Remove(e)
		delete(c.m, x.k)
		c.size -= len(x.fr.data)
	}
}

// drop removes everything of f (Close).
func (c *frameCache) drop(f *File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for e := c.ll.Front(); e != nil; {
		n := e.Next()
		if x := e.Value.(*cacheEntry); x.k.f == f {
			c.ll.Remove(e)
			delete(c.m, x.k)
			c.size -= len(x.fr.data)
		}
		e = n
	}
	for e := c.decLL.Front(); e != nil; {
		n := e.Next()
		if x := e.Value.(*decEntry); x.k.f == f {
			c.decLL.Remove(e)
			delete(c.decoder, x.k)
		}
		e = n
	}
}

// decoderFor returns the decoder of f's section s: the shared plain decoder when the section
// has no dictionary, else one with the section's dictionary, built on first use and kept in
// a small LRU (an evicted decoder is only dropped: decoders used through DecodeAll own no
// goroutines, and a caller still holding one finishes with it).
func (f *File) decoderFor(s int) (*zstd.Decoder, error) {
	sec := &f.sec[s]
	if s == SecWrappers || sec.preludeLen == 0 {
		return plainDecoder()
	}
	k := decKey{f, s}
	cache.mu.Lock()
	if e, ok := cache.decoder[k]; ok {
		cache.decLL.MoveToFront(e)
		cache.mu.Unlock()
		return e.Value.(*decEntry).d, nil
	}
	cache.mu.Unlock()
	dict, err := f.prelude(s)
	if err != nil {
		return nil, err
	}
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderDicts(dict), zstd.WithDecodeAllCapLimit(true))
	if err != nil {
		return nil, fmt.Errorf("%w: section %s dictionary: %v", errCorrupt, SectionNames[s], err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if e, ok := cache.decoder[k]; ok { // another goroutine built it meanwhile
		return e.Value.(*decEntry).d, nil
	}
	cache.decoder[k] = cache.decLL.PushFront(&decEntry{k, d})
	for cache.decLL.Len() > maxDecoders {
		e := cache.decLL.Back()
		cache.decLL.Remove(e)
		delete(cache.decoder, e.Value.(*decEntry).k)
	}
	return d, nil
}
