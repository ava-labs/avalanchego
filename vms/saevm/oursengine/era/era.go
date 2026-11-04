// Package era reads the tradenode era pack format (base snap ERASNAP2 and stream ERASTRM1),
// copied from faster-firewood-trie/era with every stream field kept, written from tradenode eras-pack `era/src/lib.rs` (HEAD 4dc0bda). Base and overlay content:
// 64-byte header, state frames of about 4 KiB of records sorted by (keccak(address), kind,
// keccak(slot)), code frames, index (77 B per frame: 65-byte first key, u64 offset, u32 length),
// wiped list, 64-byte footer. Files may carry the casfs trailer; chunk hashes are not checked
// here (the trie root check proves the content).
package era

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
)

const (
	SnapMagic   = "ERASNAP2"
	StreamMagic = "ERASTRM1"
	casfsMagic  = "CASFSv1\n"
	footer      = 7*8 + 8
	Entry       = 65 + 8 + 4
	HeaderLen   = 64
)

// Record tags.
const (
	TagAccount = 0
	TagSlot    = 1
	TagContext = 2
	TagCode    = 3
	TagAbsent  = 4
)

// Content maps a file and returns its content (without a casfs trailer).
func Content(path string) ([]byte, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	n := len(b)
	if n >= 16 && string(b[n-8:]) == casfsMagic {
		l := binary.LittleEndian.Uint64(b[n-16:])
		if l > uint64(n) {
			return nil, nil, errors.New("era: casfs length past the file")
		}
		n = int(l)
	}
	return b[:n], func() error { return syscall.Munmap(b) }, nil
}

// Snap is a base or overlay content in memory (mapped or a slice).
type Snap struct {
	B       []byte
	Block   uint64
	IndexOf uint64
	NState  int
	NFrames int
}

func OpenSnap(b []byte) (*Snap, error) {
	if len(b) < HeaderLen+footer || string(b[:8]) != SnapMagic {
		return nil, errors.New("era: not an era snap")
	}
	f := b[len(b)-footer:]
	if string(f[56:]) != SnapMagic {
		return nil, errors.New("era: snap footer magic")
	}
	w := func(i int) uint64 { return binary.LittleEndian.Uint64(f[i*8:]) }
	indexOff, nState, nCode, block, wipedOff, nWiped := w(0), w(1), w(2), w(3), w(4), w(5)
	if indexOff+(nState+nCode)*Entry != wipedOff || wipedOff+32*nWiped+footer != uint64(len(b)) {
		return nil, errors.New("era: snap layout")
	}
	return &Snap{B: b, Block: block, IndexOf: indexOff, NState: int(nState), NFrames: int(nState + nCode)}, nil
}

// Frame returns frame i's first key, offset and length.
func (s *Snap) Frame(i int) (key []byte, off uint64, n uint32) {
	e := s.B[s.IndexOf+uint64(i)*Entry:]
	return e[:65], binary.LittleEndian.Uint64(e[65:]), binary.LittleEndian.Uint32(e[73:])
}

// StateEnd is the end offset of the state region (start of the code frames).
func (s *Snap) StateEnd() uint64 {
	if s.NState == s.NFrames {
		return s.IndexOf
	}
	_, off, _ := s.Frame(s.NState)
	return off
}

// Rec is one decoded record. Key fields alias the snap bytes.
type Rec struct {
	Tag      byte
	Key      []byte // account hash (0, 2, 4) or slot hash (1) or code hash (3)
	Flags    byte   // account
	Nonce    uint64 // account
	Balance  []byte // account, trimmed
	CodeHash []byte // account with code
	Value    []byte // slot, trimmed
	Code     []byte // code
	Len      int    // record length in bytes
}

var errTrunc = errors.New("era: truncated record")

func varint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < 10 && i < len(b); i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errTrunc
}

// Decode decodes the record at b[0:].
func Decode(b []byte, r *Rec) error {
	if len(b) < 33 {
		return errTrunc
	}
	r.Tag, r.Key = b[0], b[1:33]
	p := 33
	switch r.Tag {
	case TagAccount:
		if len(b) < p+1 {
			return errTrunc
		}
		r.Flags = b[p]
		p++
		v, n, err := varint(b[p:])
		if err != nil {
			return err
		}
		r.Nonce = v
		p += n
		if len(b) < p+1 || b[p] > 32 || len(b) < p+1+int(b[p]) {
			return errTrunc
		}
		r.Balance = b[p+1 : p+1+int(b[p])]
		p += 1 + int(b[p])
		r.CodeHash = nil
		if r.Flags&2 != 0 {
			if len(b) < p+32 {
				return errTrunc
			}
			r.CodeHash = b[p : p+32]
			p += 32
		}
	case TagSlot:
		if len(b) < p+1 || b[p] > 32 || len(b) < p+1+int(b[p]) {
			return errTrunc
		}
		r.Value = b[p+1 : p+1+int(b[p])]
		p += 1 + int(b[p])
	case TagContext, TagAbsent:
	case TagCode:
		if len(b) < p+4 {
			return errTrunc
		}
		n := int(binary.LittleEndian.Uint32(b[p:]))
		if len(b) < p+4+n {
			return errTrunc
		}
		r.Code = b[p+4 : p+4+n]
		p += 4 + n
	default:
		return fmt.Errorf("era: unknown record tag %d", r.Tag)
	}
	r.Len = p
	return nil
}

// --- writer (tests and fixtures) ---

const frameTarget = 4 << 10

// Writer builds snap content in memory; records must come in key order.
type Writer struct {
	data  []byte
	index [][]byte // 77-byte entries
	raw   []byte
	first []byte
	cur   []byte
	block uint64
	codes int // state frames before the first code frame; -1: no code yet
}

func NewWriter(block uint64) *Writer {
	w := &Writer{block: block, data: make([]byte, HeaderLen), codes: -1}
	copy(w.data, SnapMagic)
	binary.LittleEndian.PutUint64(w.data[8:], block)
	return w
}

func (w *Writer) start(key []byte) {
	if len(w.raw) >= frameTarget {
		w.flush()
	}
	if w.first == nil {
		w.first = append([]byte(nil), key...)
		w.cur = nil
	}
}

func (w *Writer) flush() {
	if len(w.raw) == 0 {
		return
	}
	e := make([]byte, Entry)
	copy(e, w.first)
	binary.LittleEndian.PutUint64(e[65:], uint64(len(w.data)))
	binary.LittleEndian.PutUint32(e[73:], uint32(len(w.raw)))
	w.index = append(w.index, e)
	w.data = append(w.data, w.raw...)
	w.raw, w.first, w.cur = w.raw[:0], nil, nil
}

func trimmed(dst, v []byte) []byte {
	for len(v) > 0 && v[0] == 0 {
		v = v[1:]
	}
	dst = append(dst, byte(len(v)))
	return append(dst, v...)
}

// Account adds an account record; codeHash nil = no code. balance is big-endian.
func (w *Writer) Account(ah [32]byte, nonce uint64, balance []byte, codeHash []byte, multicoin bool) {
	k := make([]byte, 65)
	copy(k, ah[:])
	w.start(k)
	w.raw = append(w.raw, TagAccount)
	w.raw = append(w.raw, ah[:]...)
	fl := byte(0)
	if multicoin {
		fl |= 1
	}
	if codeHash != nil {
		fl |= 2
	}
	w.raw = append(w.raw, fl)
	w.raw = binary.AppendUvarint(w.raw, nonce)
	w.raw = trimmed(w.raw, balance)
	if codeHash != nil {
		w.raw = append(w.raw, codeHash...)
	}
	w.cur = append(w.cur[:0], ah[:]...)
}

// Slot adds a non-zero word of account ah.
func (w *Writer) Slot(ah, sh [32]byte, v []byte) {
	k := make([]byte, 65)
	copy(k, ah[:])
	k[32] = 1
	copy(k[33:], sh[:])
	w.start(k)
	if string(w.cur) != string(ah[:]) {
		w.raw = append(w.raw, TagContext)
		w.raw = append(w.raw, ah[:]...)
		w.cur = append(w.cur[:0], ah[:]...)
	}
	w.raw = append(w.raw, TagSlot)
	w.raw = append(w.raw, sh[:]...)
	w.raw = trimmed(w.raw, v)
}

// Code adds a code record in code frames (index key: the code hash); call it after every state
// record, in code-hash order.
func (w *Writer) Code(ch [32]byte, code []byte) {
	if w.codes < 0 {
		w.flush()
		w.codes = len(w.index)
	}
	k := make([]byte, 65)
	copy(k, ch[:])
	w.start(k)
	w.raw = append(w.raw, TagCode)
	w.raw = append(w.raw, ch[:]...)
	w.raw = binary.LittleEndian.AppendUint32(w.raw, uint32(len(code)))
	w.raw = append(w.raw, code...)
}

// Finish returns the content (no wiped list).
func (w *Writer) Finish() []byte {
	w.flush()
	nState := len(w.index)
	if w.codes >= 0 {
		nState = w.codes
	}
	indexOff := uint64(len(w.data))
	for _, e := range w.index {
		w.data = append(w.data, e...)
	}
	wipedOff := uint64(len(w.data))
	for _, v := range []uint64{indexOff, uint64(nState), uint64(len(w.index) - nState), w.block, wipedOff, 0, 0} {
		w.data = binary.LittleEndian.AppendUint64(w.data, v)
	}
	return append(w.data, SnapMagic...)
}
