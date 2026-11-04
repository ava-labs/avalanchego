// Package tier0 writes and reads the tier0 data file: the history unit of 65,536 txs.
//
// A tier0 holds the blocks from its first block to the first block boundary at or after
// CutTxs txs. The encoding is canonical: the same blocks give the same bytes on any machine
// (fixed section order, write sets sorted, no timestamps, no node-specific bytes). The file is
// a chunkfile (1 MB chunks, hash list header); its name is the chunkfile hash.
//
// Data part layout, format v6 (after the chunkfile header; integers little-endian):
//
//	header  32 B: magic "FFTIER0\x06" | first block u64 | blocks u32 | txs u32 |
//	        first tx ordinal u64 (chain-wide, 0 = the first tx ever)
//	sections in fixed order (bodies, wrappers, receipts, write sets, outputs, codes), each:
//	        prelude (zstd frame without a dictionary, may be absent: the section's zstd
//	        dictionary, or for the wrappers the certificate table) | frames
//	head    one zstd frame without a dictionary:
//	        per block uvarint tx count |
//	        per section: uvarint prelude bytes | uvarint prelude raw bytes |
//	        uvarint frames | per frame uvarint blocks | uvarint raw bytes | uvarint bytes
//	trailer 8 B: head bytes u32 | head raw bytes u32
//
// A frame is one zstd frame (with the section's dictionary when it has one) of consecutive
// block records, each as uvarint length | record; it closes after the record that brings it
// to frameRaw (128 KiB) raw bytes or more, so a block's record costs one frame to decode.
// The parameters (level, frame size, dictionary sizes, training subset) are in zframe.go and
// belong to the format, as does the pinned zstd library version: a library bump that changes
// encoder output is a format version bump (file hashes change). Format v5 (raw records with
// per-section offset tables, magic "FFTIER0\x05") is still read.
//
// Block records:
//
//	bodies:     the inner block bytes (for C-Chain: the coreth block RLP)
//	wrappers:   empty when the block has no wrapper; else uvarint len(pre) | pre | post, where
//	            container = pre | body | post byte for byte (proposervm: pre ends with the
//	            u32 length of the inner block, post is what follows it, the signature);
//	            stored with the proposer certificate taken out into the file's certificate
//	            table (see certSplit), Record returns this form
//	receipts:   uvarint ntx, then per tx uvarint length + receipt in storage form: the
//	            consensus encoding without the bloom (see receipt.go; rebuilt on read)
//	write sets: uvarint ngroups (= ntx+2: pre-tx ops, one group per tx, end-of-block ops),
//	            per group: uvarint naddrs, then per address (ascending) address 20 |
//	            uvarint nwrites (>= 1) | writes sorted by (kind, slot), each kind 1 |
//	            slot 32 (kind 0 only) | old | new, a value being u8 length (0-32) + its
//	            big-endian bytes without leading zeros
//	outputs:    uvarint ntx, then per tx uvarint length + return data or revert reason
//	codes:      uvarint n, then per code uvarint length + bytes: the code of every code hash
//	            a write of the block sets (not 0, not keccak256("")), once each, sorted by
//	            code hash (the state needs the bytes; write sets carry only the hash)
//
// Write kinds: 0 storage slot, 1 balance, 2 nonce, 3 code hash, 4 multicoin flag (0 or 1).
// Account fields are pseudo-keys with 32-byte big-endian values; code hash 0 means the
// account does not exist, so a code hash write to 0 (a deletion: legacy SELFDESTRUCT, EIP-161)
// also drops the account's whole storage and coin balances, written or not; an account without
// code has keccak256(""). A slot is the state
// key: an EVM slot normalized (bit 248 cleared), a multicoin balance its coin id (bit 248
// set), as coreth's storage trie keys them. A write is listed only when old != new, and a
// key at most once per group.
//
// Each file has a sidecar <hash>.facts with the raw per-block facts the index step consumes,
// see Facts, until its index piece is built: the pipeline then removes it (as big as the file
// itself; piece.FileFacts derives the same records from the file).
//
// The open tier0 lives on disk in the writer's directory, never in RAM beyond 4 B per block
// per section: bodies.open (the bodies store, CRC-framed records of body and wrapper, the
// restart source: the block's container as the peer delivered it) and one plain append spool
// per other section and for the facts. Nothing is fsynced before the cut:
// after a crash the node re-executes the open tier0 from the bodies store (OpenWriter drops a
// torn tail and truncates the spools; the re-executed blocks rebuild them). At the cut the
// spools are sealed and the file is written from them, on the side with AddSeal and Seal (see
// seal.go); Recover finishes the cuts a crash interrupted.
package tier0

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/crypto/sha3"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// CutTxs is the tier0 size: a file is cut at the first block boundary at or after it.
const CutTxs = 1 << 16

// cutTxs is where a writer cuts: CutTxs; only a build with the tag inject changes it (SetCutTxs,
// the tip-fold box tests: a fold at the tip needs a cut there).
var cutTxs = CutTxs

// CutAt is the tx count at which the open tier0 is cut (CutTxs but in an inject-tag test build).
func CutAt() int { return cutTxs }

// Sections in file order.
const (
	SecBodies = iota
	SecWrappers
	SecReceipts
	SecWrites
	SecOutputs
	SecCodes
	nsec
)

var SectionNames = [nsec]string{"bodies", "wrappers", "receipts", "writes", "outputs", "codes"}

const (
	magic       = "FFTIER0\x06"
	magic5      = "FFTIER0\x05" // raw records, still read
	factsMagic  = "FFT0FCT\x04"
	factsMagic3 = "FFT0FCT\x03" // before the nonce writers: still read
	factsMagic2 = "FFT0FCT\x02" // before the block hash and the code-hash writers: still read
	headerSize  = 8 + 8 + 4 + 4 + 8
	headerSize5 = headerSize + nsec*8
	trailerSize = 8
	factsHead   = 8 + 8 + 4 + 8
	bodiesName  = "bodies.open"
	factsName   = "facts.spool"
)

// Write kinds.
const (
	KindSlot    = 0
	KindBalance = 1
	KindNonce   = 2
	KindCode    = 3
	KindMulti   = 4 // the sticky coreth multicoin flag (an empty multicoin account exists)
)

// validWrite checks a write's kind and, for the multicoin flag, its values.
func validWrite(w *Write) bool {
	switch {
	case w.Kind > KindMulti:
		return false
	case w.Kind == KindMulti:
		return [31]byte(w.Old[:31]) == [31]byte{} && [31]byte(w.New[:31]) == [31]byte{} && w.Old[31] <= 1 && w.New[31] <= 1
	}
	return true
}

// Write is one changed key of a tx.
type Write struct {
	Addr     [20]byte
	Kind     byte
	Slot     [32]byte // KindSlot only
	Old, New [32]byte
}

func (w *Write) key() []byte {
	k := append(w.Addr[:], w.Kind)
	if w.Kind == KindSlot {
		k = append(k, w.Slot[:]...)
	}
	return k
}

// cmpWrite is bytes.Compare(a.key(), b.key()) without building the keys (key allocates: the
// sorts and checks below compare every write several times).
func cmpWrite(a, b *Write) int {
	if c := bytes.Compare(a.Addr[:], b.Addr[:]); c != 0 {
		return c
	}
	if a.Kind != b.Kind {
		return cmp.Compare(a.Kind, b.Kind)
	}
	if a.Kind == KindSlot {
		return bytes.Compare(a.Slot[:], b.Slot[:])
	}
	return 0
}

// SortWrites puts writes in canonical order (address, kind, slot).
func SortWrites(ws []Write) {
	slices.SortFunc(ws, func(a, b Write) int { return cmpWrite(&a, &b) })
}

// Wrapper is what surrounds the inner block in the block's container: container = Pre | body |
// Post. For a proposervm block Pre is the codec bytes up to and including the inner block's
// u32 length and Post the bytes after the inner block.
type Wrapper struct {
	Pre, Post []byte
}

// Block is what executing one block produced.
type Block struct {
	Number    uint64
	Hash      [32]byte // the header hash (keccak256 of the header RLP), for the facts sidecar; the caller has it from its gate
	Body      []byte
	Wrapper   *Wrapper // nil: the block has no wrapper
	TxHashes  [][32]byte
	Receipts  [][]byte  // consensus encoding, one per tx
	Outputs   [][]byte  // one per tx
	Writes    [][]Write // len(TxHashes)+2: pre-tx ops, one group per tx, end-of-block ops; sorted in place by Add
	Codes     [][]byte  // the code of every code hash the writes set (see the codes section), any order
	LogAddrs  [][20]byte
	LogTopics [][32]byte
}

// Cut describes a written tier0 file.
type Cut struct {
	Path         string // <dir>/<hash hex>.tier0; the sidecar is <dir>/<hash hex>.facts
	Hash         chunkfile.Hash
	First, Last  uint64
	FirstOrdinal uint64
	Txs          int
}

// stored is a record found in the bodies store at open: the block re-executed from it must
// give the same body and wrapper.
type stored struct {
	number uint64
	n      int
	crc    uint32
}

// storeRec is one bodies store record: u32 LE n (bytes up to the CRC) | u64 LE number | u32 LE
// length of the wrapper record | wrapper record (as in the wrappers section) | body | u32 LE
// CRC-32C of everything after n.
func storeRec(number uint64, wrec, body []byte) []byte {
	r := make([]byte, 16, 20+len(wrec)+len(body))
	binary.LittleEndian.PutUint32(r, uint32(12+len(wrec)+len(body)))
	binary.LittleEndian.PutUint64(r[4:], number)
	binary.LittleEndian.PutUint32(r[12:], uint32(len(wrec)))
	r = append(append(r, wrec...), body...)
	return binary.LittleEndian.AppendUint32(r, crc32.Checksum(r[4:], crcTab))
}

// wrapperRec is the wrappers section record of wr.
func wrapperRec(wr *Wrapper) []byte {
	if wr == nil {
		return nil
	}
	x := binary.AppendUvarint(nil, uint64(len(wr.Pre)))
	return append(append(x, wr.Pre...), wr.Post...)
}

// parseWrapper decodes a wrappers section record (nil: no wrapper).
func parseWrapper(r []byte) (*Wrapper, error) {
	if len(r) == 0 {
		return nil, nil
	}
	c := &cursor{b: r}
	pre := c.take(c.uvarint())
	if c.err != nil {
		return nil, c.err
	}
	return &Wrapper{Pre: pre, Post: c.b}, nil
}

// Writer is the open tier0.
type Writer struct {
	dir string
	spools
	stored []stored
	jobs   chan *Sealed // sealed tier0s for the final writer, in order
	wg     sync.WaitGroup
	mu     sync.Mutex
	failed error // a final write failed: no later tier0 is finalized (the cut files stay contiguous)
}

// spools is one tier0's records on disk, the open one or a sealed one: the bodies store, one
// append spool per other section and the facts spool (a facts sidecar once its header is
// written at the seal), with the tables that locate each block's records (4 B per block per
// section) and running CRC-32Cs that let Recover tell an intact sealed tier0 from a torn one.
type spools struct {
	dir      string // the open tier0: the writer's directory; a sealed one: its sealed directory
	bodies   *os.File
	spool    [nsec]*os.File // nil for SecBodies (the bodies store holds them)
	facts    *os.File
	first    uint64
	ordinal  uint64 // chain-wide ordinal of the tier0's first tx
	n        int
	txs      int
	size     [nsec]uint64 // record bytes per section (bodies: the bodies alone)
	crc      [nsec]uint32 // CRC-32C of each spool's bytes; SecBodies: of the bodies store
	off      [nsec][]uint32
	cum      []uint32 // txs before each block of the tier0
	bodyAt   []int64  // offset of each block's record in the bodies store
	storeAt  int64    // offset of the next block's record (the store's size)
	factsAt  int64    // the facts spool's size, header included
	factsCRC uint32   // CRC-32C of the facts records (after the header)
}

var crcTab = crc32.MakeTable(crc32.Castagnoli)

// OpenWriter opens the open tier0 in dir; next is the first block not yet in a cut tier0 and
// ordinal the chain-wide ordinal of its first tx (the last cut's first ordinal + its txs).
// Bodies stored from next on are kept for re-execution (see Stored); a store left from an
// already cut tier0 (a crash between the cut and the reset) is dropped.
func OpenWriter(dir string, next, ordinal uint64) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if left, err := sealedDirs(dir); err != nil {
		return nil, err
	} else if len(left) > 0 {
		return nil, fmt.Errorf("tier0: %s holds sealed tier0 %s: call Recover before computing the next block", dir, filepath.Base(left[0]))
	}
	os.Remove(filepath.Join(dir, "cut.tier0.data")) // a cut interrupted by a crash (writers before the seal)
	os.Remove(filepath.Join(dir, "cut.tier0.part"))
	w := &Writer{dir: dir}
	if err := w.openSpools(dir, ordinal, false); err != nil {
		return nil, err
	}
	end, err := w.scanBodies(next)
	if err == nil {
		err = w.bodies.Truncate(end)
	}
	if err == nil {
		_, err = w.bodies.Seek(end, io.SeekStart)
	}
	if err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

// openSpools opens the open tier0's files in dir, empty but for the bodies store unless
// fresh is false (OpenWriter scans the store it finds); the facts spool starts with room for
// the sidecar header.
func (sp *spools) openSpools(dir string, ordinal uint64, fresh bool) error {
	*sp = spools{dir: dir, ordinal: ordinal, factsAt: factsHead}
	flag := os.O_RDWR | os.O_CREATE
	if fresh {
		flag |= os.O_TRUNC
	}
	var err error
	sp.bodies, err = os.OpenFile(filepath.Join(dir, bodiesName), flag, 0o644)
	for s := range nsec {
		if err == nil && s != SecBodies {
			sp.spool[s], err = os.OpenFile(filepath.Join(dir, SectionNames[s]+".spool"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		}
	}
	if err == nil {
		sp.facts, err = os.OpenFile(filepath.Join(dir, factsName), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	}
	if err == nil {
		_, err = sp.facts.Write(make([]byte, factsHead))
	}
	if err != nil {
		sp.close()
	}
	return err
}

// scanBodies reads the store's intact consecutive records from next on and returns where they
// end; anything from a torn, corrupt or out-of-sequence record on is dropped.
func (w *Writer) scanBodies(next uint64) (int64, error) {
	r := bufio.NewReaderSize(io.NewSectionReader(w.bodies, 0, math.MaxInt64), 1<<20)
	var end int64
	var buf []byte
	for {
		num, wrec, body, crc, err := readRec(r, &buf)
		if err != nil {
			return end, nil
		}
		if num != next+uint64(len(w.stored)) {
			if len(w.stored) == 0 && num < next {
				return 0, nil // the store belongs to a tier0 that was cut: start empty
			}
			if len(w.stored) == 0 {
				return 0, fmt.Errorf("tier0: bodies store starts at block %d, next block is %d", num, next)
			}
			return end, nil
		}
		n := 20 + len(wrec) + len(body)
		w.stored = append(w.stored, stored{num, n, crc})
		end += int64(n)
	}
}

var errTorn = errors.New("tier0: torn bodies store record")

// readRec reads one bodies store record (see storeRec) into *bufp; wrec and body alias it.
func readRec(r *bufio.Reader, bufp *[]byte) (num uint64, wrec, body []byte, crc uint32, err error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, nil, 0, err
	}
	n := int(binary.LittleEndian.Uint32(h[:]))
	if n < 12 {
		return 0, nil, nil, 0, errTorn
	}
	buf := slices.Grow((*bufp)[:0], n+4)[:n+4]
	*bufp = buf
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, nil, 0, err
	}
	crc = crc32.Checksum(buf[:n], crcTab)
	wl := int(binary.LittleEndian.Uint32(buf[8:]))
	if crc != binary.LittleEndian.Uint32(buf[n:]) || wl > n-12 {
		return 0, nil, nil, 0, errTorn
	}
	return binary.LittleEndian.Uint64(buf), buf[12 : 12+wl : 12+wl], buf[12+wl : n : n], crc, nil
}

// Stored calls f with every block in the store at open, in order: the blocks to re-execute
// before new ones (their results then go to Add as usual, with this body and wrapper). body
// and wrapper are reused after f returns.
func (w *Writer) Stored(f func(number uint64, body []byte, wrapper *Wrapper) error) error {
	r := bufio.NewReaderSize(io.NewSectionReader(w.bodies, 0, math.MaxInt64), 1<<20)
	var buf []byte
	for range w.stored {
		num, wrec, body, _, err := readRec(r, &buf)
		if err != nil {
			return err
		}
		wr, err := parseWrapper(wrec)
		if err != nil {
			return fmt.Errorf("tier0: stored block %d: wrapper: %w", num, err)
		}
		if err := f(num, body, wr); err != nil {
			return err
		}
	}
	return nil
}

// NumStored is the number of blocks the store held when the writer was opened.
func (w *Writer) NumStored() int { return len(w.stored) }

// Txs is the tx count of the open tier0.
func (w *Writer) Txs() int { return w.txs }

// First is the open tier0's first block (meaningful when Blocks() > 0).
func (w *Writer) First() uint64 { return w.first }

// Blocks is the number of blocks in the open tier0.
func (w *Writer) Blocks() int { return w.n }

// Record is block i's record of section s in the open tier0 (i from 0, block First()+i), the
// bytes File.Record gives once the tier0 is cut: read back from the spool (the body from the
// bodies store, its CRC checked). The caller serializes it with Add and Cut.
func (w *Writer) Record(s, i int) ([]byte, error) {
	if i < 0 || i >= w.n {
		return nil, fmt.Errorf("tier0: open block position %d of %d", i, w.n)
	}
	if s == SecBodies {
		var buf []byte
		num, _, body, _, err := readRec(bufio.NewReader(io.NewSectionReader(w.bodies, w.bodyAt[i], math.MaxInt64-w.bodyAt[i])), &buf)
		if err != nil {
			return nil, err
		}
		if num != w.first+uint64(i) {
			return nil, fmt.Errorf("tier0: bodies store record %d is block %d", i, num)
		}
		return body, nil
	}
	end := uint32(w.size[s])
	if i+1 < w.n {
		end = w.off[s][i+1]
	}
	b := make([]byte, end-w.off[s][i])
	if _, err := w.spool[s].ReadAt(b, int64(w.off[s][i])); err != nil {
		return nil, err
	}
	return b, nil
}

// Add appends one block. When the block brings the open tier0 to CutTxs txs or more, the file
// is cut and described by the returned Cut: AddSeal, then the final file written and the
// sealed spools released before Add returns. An error leaves the writer unusable (reopen it).
func (w *Writer) Add(b *Block) (*Cut, error) {
	s, err := w.AddSeal(b)
	if err != nil || s == nil {
		return nil, err
	}
	return s.finish()
}

// AddSeal appends one block. When the block brings the open tier0 to CutTxs txs or more, the
// tier0 is sealed (see Seal) and returned; its final file is written on the side. An error
// leaves the writer unusable (reopen it).
func (w *Writer) AddSeal(b *Block) (*Sealed, error) {
	if err := w.err(); err != nil {
		return nil, err
	}
	ntx := len(b.TxHashes)
	if len(b.Receipts) != ntx || len(b.Outputs) != ntx || len(b.Writes) != ntx+2 {
		return nil, fmt.Errorf("tier0: block %d: %d txs, %d receipts, %d outputs, %d write groups", b.Number, ntx, len(b.Receipts), len(b.Outputs), len(b.Writes))
	}
	if w.n > 0 && b.Number != w.first+uint64(w.n) || w.n == 0 && len(w.stored) > 0 && b.Number != w.stored[0].number {
		return nil, fmt.Errorf("tier0: block %d out of sequence", b.Number)
	}
	if w.n == 0 {
		w.first = b.Number
	}
	recs := [nsec][]byte{SecBodies: b.Body, SecWrappers: wrapperRec(b.Wrapper)}
	rs := make([][]byte, len(b.Receipts))
	for i, r := range b.Receipts {
		var err error
		if rs[i], err = storeReceipt(r); err != nil {
			return nil, fmt.Errorf("block %d tx %d: %w", b.Number, i, err)
		}
	}
	recs[SecReceipts] = appendList(nil, rs)
	recs[SecOutputs] = appendList(nil, b.Outputs)
	ws := binary.AppendUvarint(nil, uint64(len(b.Writes)))
	for _, g := range b.Writes {
		SortWrites(g)
		naddr := 0
		for i := range g {
			if i == 0 || g[i].Addr != g[i-1].Addr {
				naddr++
			}
		}
		ws = binary.AppendUvarint(ws, uint64(naddr))
		for i := range g {
			x := &g[i]
			if i > 0 && cmpWrite(&g[i-1], x) == 0 { // one write per key per group, else the order is not canonical
				return nil, fmt.Errorf("tier0: block %d: key %x written twice in one group", b.Number, x.key())
			}
			if !validWrite(x) {
				return nil, fmt.Errorf("tier0: block %d: write kind %d (%x -> %x)", b.Number, x.Kind, x.Old, x.New)
			}
			if i == 0 || x.Addr != g[i-1].Addr {
				n := 1
				for i+n < len(g) && g[i+n].Addr == x.Addr {
					n++
				}
				ws = binary.AppendUvarint(append(ws, x.Addr[:]...), uint64(n))
			}
			ws = append(ws, x.Kind)
			if x.Kind == KindSlot {
				ws = append(ws, x.Slot[:]...)
			}
			ws = appendValue(appendValue(ws, &x.Old), &x.New)
		}
	}
	recs[SecWrites] = ws
	cr, err := codesRec(b)
	if err != nil {
		return nil, err
	}
	recs[SecCodes] = cr
	for s := range nsec {
		if w.size[s]+uint64(len(recs[s])) > math.MaxUint32 {
			return nil, fmt.Errorf("tier0: section %s over 4 GiB", SectionNames[s])
		}
	}
	r := storeRec(b.Number, recs[SecWrappers], b.Body)
	if w.n < len(w.stored) {
		if s := w.stored[w.n]; s.n != len(r) || s.crc != binary.LittleEndian.Uint32(r[len(r)-4:]) {
			return nil, fmt.Errorf("tier0: block %d: body or wrapper differs from the stored block it was re-executed from", b.Number)
		}
	} else if _, err := w.bodies.Write(r); err != nil { // one write: a kill leaves at most a torn tail
		return nil, err
	}
	w.crc[SecBodies] = crc32.Update(w.crc[SecBodies], crcTab, r)
	w.cum = append(w.cum, uint32(w.txs))
	w.bodyAt = append(w.bodyAt, w.storeAt)
	w.storeAt += int64(len(r))
	for s := range nsec {
		w.off[s] = append(w.off[s], uint32(w.size[s]))
		w.size[s] += uint64(len(recs[s]))
		if s != SecBodies {
			if _, err := w.spool[s].Write(recs[s]); err != nil {
				return nil, err
			}
			w.crc[s] = crc32.Update(w.crc[s], crcTab, recs[s])
		}
	}
	fa := FactsOf(b)
	fr := appendFacts(nil, &fa)
	if _, err := w.facts.Write(fr); err != nil {
		return nil, err
	}
	w.factsCRC = crc32.Update(w.factsCRC, crcTab, fr)
	w.factsAt += int64(len(fr))
	w.n++
	w.txs += ntx
	if w.txs >= cutTxs {
		return w.Seal()
	}
	return nil, nil
}

// appendValue appends a 32-byte value as u8 length + bytes without leading zeros.
func appendValue(dst []byte, v *[32]byte) []byte {
	t := bytes.TrimLeft(v[:], "\x00")
	return append(append(dst, byte(len(t))), t...)
}

var emptyCode = keccak(nil)

func keccak(b []byte) (h [32]byte) {
	k := sha3.NewLegacyKeccak256()
	k.Write(b)
	k.Sum(h[:0])
	return h
}

// codesRec is the codes record of b: its codes sorted by hash, which must be exactly the code
// hashes its writes set.
func codesRec(b *Block) ([]byte, error) {
	want := map[[32]byte]bool{}
	for _, g := range b.Writes {
		for _, x := range g {
			if x.Kind == KindCode && x.New != ([32]byte{}) && x.New != emptyCode {
				want[x.New] = true
			}
		}
	}
	type code struct {
		h [32]byte
		b []byte
	}
	cs := make([]code, len(b.Codes))
	for i, c := range b.Codes {
		cs[i] = code{keccak(c), c}
		if !want[cs[i].h] {
			return nil, fmt.Errorf("tier0: block %d: code %x is not set by a write or given twice", b.Number, cs[i].h)
		}
		delete(want, cs[i].h)
	}
	for h := range want {
		return nil, fmt.Errorf("tier0: block %d: no code for code hash %x", b.Number, h)
	}
	slices.SortFunc(cs, func(x, y code) int { return bytes.Compare(x.h[:], y.h[:]) })
	items := make([][]byte, len(cs))
	for i := range cs {
		items[i] = cs[i].b
	}
	return appendList(nil, items), nil
}

func appendList(dst []byte, items [][]byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(items)))
	for _, x := range items {
		dst = binary.AppendUvarint(dst, uint64(len(x)))
		dst = append(dst, x...)
	}
	return dst
}

// sorted sorts and dedups fixed-size keys in place; c is their byte order (cmp20, cmp32,
// cmp52: a compare through a func(*K) []byte made both copies escape, two allocations per
// compare).
func sorted[K comparable](ks []K, c func(x, y K) int) []K {
	slices.SortFunc(ks, c)
	return slices.Compact(ks)
}

func cmp20(x, y [20]byte) int { return bytes.Compare(x[:], y[:]) }
func cmp32(x, y [32]byte) int { return bytes.Compare(x[:], y[:]) }
func cmp52(x, y [52]byte) int { return bytes.Compare(x[:], y[:]) }

// appendKeys appends keys with a uvarint count.
func appendKeys[K any](dst []byte, ks []K, b func(*K) []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(ks)))
	for i := range ks {
		dst = append(dst, b(&ks[i])...)
	}
	return dst
}

func b20(k *[20]byte) []byte { return k[:] }
func b32(k *[32]byte) []byte { return k[:] }
func b52(k *[52]byte) []byte { return k[:] }

// FactsOf is block b's record of the .facts sidecar, as Add writes it: the index step's
// input (see Facts). The sets are b's written accounts, slots, log addresses and topics, and
// code-hash and nonce writers, each sorted and unique; TxHashes is b's slice.
func FactsOf(b *Block) Facts {
	f := Facts{Number: b.Number, TxHashes: b.TxHashes, Hash: b.Hash}
	for _, g := range b.Writes {
		for i := range g {
			x := &g[i]
			switch x.Kind {
			case KindSlot:
				var k [52]byte
				copy(k[:], x.Addr[:])
				copy(k[20:], x.Slot[:])
				f.Slots = append(f.Slots, k)
				continue
			case KindCode:
				f.CodeAccts = append(f.CodeAccts, x.Addr)
			case KindNonce:
				f.NonceAccts = append(f.NonceAccts, x.Addr)
			}
			f.Accts = append(f.Accts, x.Addr)
		}
	}
	f.Accts = sorted(f.Accts, cmp20)
	f.Slots = sorted(f.Slots, cmp52)
	f.LogAddrs = sorted(slices.Clone(b.LogAddrs), cmp20)
	f.LogTopics = sorted(slices.Clone(b.LogTopics), cmp32)
	f.CodeAccts = sorted(f.CodeAccts, cmp20)
	f.NonceAccts = sorted(f.NonceAccts, cmp20)
	return f
}

// appendFacts appends a facts record (sidecar format v4).
func appendFacts(dst []byte, f *Facts) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(f.TxHashes)))
	for _, h := range f.TxHashes {
		dst = append(dst, h[:]...)
	}
	dst = appendKeys(dst, f.Accts, b20)
	dst = appendKeys(dst, f.Slots, b52)
	dst = appendKeys(dst, f.LogAddrs, b20)
	dst = appendKeys(dst, f.LogTopics, b32)
	dst = append(dst, f.Hash[:]...)
	dst = appendKeys(dst, f.CodeAccts, b20)
	return appendKeys(dst, f.NonceAccts, b20)
}

// Cut writes the open tier0 as the chunkfile <hash>.tier0 plus its <hash>.facts sidecar (both
// fsynced, then the directory) and starts an empty one: Seal, then the final file written and
// the sealed spools released before Cut returns. Add cuts at CutTxs by itself; calling Cut
// earlier is for tests and fixture runs only (a real tier0 is never cut early).
func (w *Writer) Cut() (*Cut, error) {
	s, err := w.Seal()
	if err != nil {
		return nil, err
	}
	return s.finish()
}

// txc is the tx count of each block.
func (sp *spools) txc() []int {
	txc := make([]int, sp.n)
	for i := range sp.n {
		next := uint32(sp.txs)
		if i+1 < sp.n {
			next = sp.cum[i+1]
		}
		txc[i] = int(next - sp.cum[i])
	}
	return txc
}

// writeV6 writes a v6 data file: records(s) gives section s's raw records (one per block).
// The sections are encoded side by side (dictionary training is the long part of a cut and
// each section trains its own) and written in order; the bytes do not depend on the order
// the encodings finish in.
func writeV6(path string, first uint64, txs int, ordinal uint64, txc []int, records func(s int) ([][]byte, error)) (chunkfile.Hash, error) {
	var encs [nsec]*encSection
	var errs [nsec]error
	var wg sync.WaitGroup
	for s := range nsec {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs, err := records(s)
			if err == nil && len(recs) != len(txc) {
				err = fmt.Errorf("tier0: section %s has %d records for %d blocks", SectionNames[s], len(recs), len(txc))
			}
			if err == nil {
				encs[s], err = encodeSection(s, recs)
			}
			if err != nil {
				errs[s] = fmt.Errorf("tier0: section %s: %w", SectionNames[s], err)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs[:]...); err != nil {
		return chunkfile.Hash{}, err
	}
	cw, err := chunkfile.Create(path)
	if err != nil {
		return chunkfile.Hash{}, err
	}
	bw := bufio.NewWriterSize(cw, 1<<20)
	h := make([]byte, headerSize)
	copy(h, magic)
	binary.LittleEndian.PutUint64(h[8:], first)
	binary.LittleEndian.PutUint32(h[16:], uint32(len(txc)))
	binary.LittleEndian.PutUint32(h[20:], uint32(txs))
	binary.LittleEndian.PutUint64(h[24:], ordinal)
	bw.Write(h)
	var head []byte
	for _, n := range txc {
		head = binary.AppendUvarint(head, uint64(n))
	}
	for _, e := range encs {
		head = binary.AppendUvarint(head, uint64(len(e.prelude)))
		head = binary.AppendUvarint(head, uint64(e.preludeRaw))
		head = binary.AppendUvarint(head, uint64(len(e.frames)))
		bw.Write(e.prelude)
		for k, fr := range e.frames {
			head = binary.AppendUvarint(head, uint64(e.blocks[k]))
			head = binary.AppendUvarint(head, uint64(e.raws[k]))
			head = binary.AppendUvarint(head, uint64(len(fr)))
			bw.Write(fr)
		}
	}
	hz := plainEncoder().EncodeAll(head, nil)
	if len(hz) > math.MaxUint32 || len(head) > math.MaxUint32 {
		return chunkfile.Hash{}, errors.New("tier0: head over 4 GiB")
	}
	bw.Write(hz)
	t := binary.LittleEndian.AppendUint32(nil, uint32(len(hz)))
	bw.Write(binary.LittleEndian.AppendUint32(t, uint32(len(head))))
	if err := bw.Flush(); err != nil {
		return chunkfile.Hash{}, err
	}
	return cw.Finish()
}

// records reads section s back from its spool (the bodies from the store).
func (w *spools) records(s int) ([][]byte, error) {
	var buf []byte
	if s == SecBodies {
		var b bytes.Buffer
		b.Grow(int(w.size[s]))
		if err := w.copyBodies(&b); err != nil {
			return nil, err
		}
		buf = b.Bytes()
	} else {
		buf = make([]byte, w.size[s])
		if _, err := w.spool[s].ReadAt(buf, 0); err != nil {
			return nil, err
		}
	}
	recs := make([][]byte, w.n)
	for i := range recs {
		end := uint32(w.size[s])
		if i+1 < w.n {
			end = w.off[s][i+1]
		}
		recs[i] = buf[w.off[s][i]:end:end]
	}
	return recs, nil
}

// copyBodies writes every body from the store, checking each record again.
func (w *spools) copyBodies(dst io.Writer) error {
	r := bufio.NewReaderSize(io.NewSectionReader(w.bodies, 0, math.MaxInt64), 1<<20)
	var buf []byte
	for i := range w.n {
		num, _, body, _, err := readRec(r, &buf)
		if err != nil {
			return fmt.Errorf("tier0: bodies store record %d: %w", i, err)
		}
		end := uint32(w.size[SecBodies])
		if i+1 < w.n {
			end = w.off[SecBodies][i+1]
		}
		if num != w.first+uint64(i) || uint32(len(body)) != end-w.off[SecBodies][i] {
			return fmt.Errorf("tier0: bodies store record %d is block %d with %d bytes", i, num, len(body))
		}
		if _, err := dst.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// tableSize5 is the size of a v5 section's head for n blocks: the offset table, plus the tx
// count column for the bodies.
func tableSize5(s, n int) uint64 {
	if s == SecBodies {
		return 8 * uint64(n+1)
	}
	return 4 * uint64(n+1)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// Close waits for the final files of the sealed tier0s (their Sealed stays readable until
// Release) and closes the open tier0's files; the open tier0 stays on disk for the next
// OpenWriter.
func (w *Writer) Close() error {
	if w.jobs != nil {
		close(w.jobs)
		w.wg.Wait()
		w.jobs = nil
	}
	return w.close()
}

// close closes the files.
func (sp *spools) close() error {
	var err error
	for _, f := range append(sp.spool[:], sp.bodies, sp.facts) {
		if f != nil {
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
	}
	return err
}

// Rebuild writes src's blocks as a current-format file <hash>.tier0 in dir, with the facts
// sidecar at factsPath copied to <hash>.facts (its bytes do not depend on the data format; no
// copy when src has none any more, its piece is built from the file then, piece.FileFacts):
// the result is byte-identical to a fresh write of the same blocks, so a directory of older
// files converts without re-execution. Returns the new file.
func Rebuild(src *File, factsPath, dir string) (*Cut, error) {
	txc := make([]int, src.Blocks)
	for i := range txc {
		txc[i] = int(src.cum[i+1] - src.cum[i])
	}
	tmp := filepath.Join(dir, "rebuild.tier0.part")
	hash, err := writeV6(tmp, src.First, src.Txs, src.FirstOrdinal, txc, func(s int) ([][]byte, error) {
		recs := make([][]byte, src.Blocks)
		q := src.Seq()
		for i := range recs {
			var err error
			if recs[i], err = q.Record(s, i); err != nil {
				return nil, fmt.Errorf("tier0: block %d section %s: %w", src.First+uint64(i), SectionNames[s], err)
			}
		}
		return recs, nil
	})
	if err != nil {
		os.Remove(tmp)
		os.Remove(tmp + ".data")
		return nil, err
	}
	name := filepath.Join(dir, hex.EncodeToString(hash[:]))
	if err := os.Rename(tmp, name+".tier0"); err != nil {
		return nil, err
	}
	if err := copyFile(factsPath, name+".facts"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := syncDir(dir); err != nil {
		return nil, err
	}
	return &Cut{Path: name + ".tier0", Hash: hash, First: src.First, Last: src.First + uint64(src.Blocks) - 1, FirstOrdinal: src.FirstOrdinal, Txs: src.Txs}, nil
}

// copyFile copies src to dst through a fsynced temp file and a rename.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
