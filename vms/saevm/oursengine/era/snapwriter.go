package era

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// SnapWriter writes snap content as it goes, for bases too big for memory: the same bytes as
// Writer for the same records. Frames go to w; their index entries (77 B each) go to idx as
// each frame is written (a file the caller keeps, so a crashed build can resume) and are
// copied after the frames by Finish. RAM is one frame.
type SnapWriter struct {
	w      io.Writer
	idx    *os.File
	ib     *bufio.Writer
	n      uint64 // content bytes written to w
	frames uint64
	codes  int64 // frames before the first code frame; -1: no code yet
	raw    []byte
	first  []byte
	cur    []byte
	block  uint64
	ent    [Entry]byte
}

// NewSnapWriter starts snap content for block: the header goes to w now.
func NewSnapWriter(w io.Writer, idx *os.File, block uint64) (*SnapWriter, error) {
	s := &SnapWriter{w: w, idx: idx, ib: bufio.NewWriterSize(idx, 64<<10), block: block, codes: -1}
	h := make([]byte, HeaderLen)
	copy(h, SnapMagic)
	binary.LittleEndian.PutUint64(h[8:], block)
	_, err := w.Write(h)
	s.n = HeaderLen
	return s, err
}

// ResumeSnapWriter continues a SnapWriter whose first frames frames (content bytes n, codes =
// Codes() then) are written to w and idx; w must append after them. The bytes that follow are
// those the uninterrupted build writes when the next record is the one it added after those
// frames.
func ResumeSnapWriter(w io.Writer, idx *os.File, block, n, frames uint64, codes int64) *SnapWriter {
	return &SnapWriter{w: w, idx: idx, ib: bufio.NewWriterSize(idx, 64<<10), block: block, n: n, frames: frames, codes: codes}
}

// Flush writes the buffered index entries to idx (before the caller fsyncs it).
func (s *SnapWriter) Flush() error { return s.ib.Flush() }

// State is what a resume needs: content bytes, frames and the first code frame, all as
// written (the open frame excluded).
func (s *SnapWriter) State() (n, frames uint64, codes int64) { return s.n, s.frames, s.codes }

// Full reports that the open frame is full: the next record starts a new frame. FlushFrame
// then writes it, the same bytes the next record's own flush would write.
func (s *SnapWriter) Full() bool { return len(s.raw) >= frameTarget }

// FlushFrame writes the open frame (nothing when empty).
func (s *SnapWriter) FlushFrame() error {
	if len(s.raw) == 0 {
		return nil
	}
	copy(s.ent[:65], s.first)
	binary.LittleEndian.PutUint64(s.ent[65:], s.n)
	binary.LittleEndian.PutUint32(s.ent[73:], uint32(len(s.raw)))
	if _, err := s.ib.Write(s.ent[:]); err != nil {
		return err
	}
	if _, err := s.w.Write(s.raw); err != nil {
		return err
	}
	s.n += uint64(len(s.raw))
	s.frames++
	s.raw, s.first, s.cur = s.raw[:0], nil, nil
	return nil
}

func (s *SnapWriter) start(key *[65]byte) error {
	if s.Full() {
		if err := s.FlushFrame(); err != nil {
			return err
		}
	}
	if s.first == nil {
		s.first = append(s.first[:0:0], key[:]...)
		s.cur = nil
	}
	return nil
}

// Account adds an account record; codeHash nil = no code. balance is big-endian.
func (s *SnapWriter) Account(ah [32]byte, nonce uint64, balance []byte, codeHash []byte, multicoin bool) error {
	var k [65]byte
	copy(k[:], ah[:])
	if err := s.start(&k); err != nil {
		return err
	}
	s.raw = append(s.raw, TagAccount)
	s.raw = append(s.raw, ah[:]...)
	fl := byte(0)
	if multicoin {
		fl |= 1
	}
	if codeHash != nil {
		fl |= 2
	}
	s.raw = append(s.raw, fl)
	s.raw = binary.AppendUvarint(s.raw, nonce)
	s.raw = trimmed(s.raw, balance)
	if codeHash != nil {
		s.raw = append(s.raw, codeHash...)
	}
	s.cur = append(s.cur[:0], ah[:]...)
	return nil
}

// Slot adds a non-zero word of account ah.
func (s *SnapWriter) Slot(ah, sh [32]byte, v []byte) error {
	var k [65]byte
	copy(k[:], ah[:])
	k[32] = 1
	copy(k[33:], sh[:])
	if err := s.start(&k); err != nil {
		return err
	}
	if string(s.cur) != string(ah[:]) {
		s.raw = append(s.raw, TagContext)
		s.raw = append(s.raw, ah[:]...)
		s.cur = append(s.cur[:0], ah[:]...)
	}
	s.raw = append(s.raw, TagSlot)
	s.raw = append(s.raw, sh[:]...)
	s.raw = trimmed(s.raw, v)
	return nil
}

// Code adds a code record in code frames; call it after every state record, in code-hash
// order.
func (s *SnapWriter) Code(ch [32]byte, code []byte) error {
	if s.codes < 0 {
		if err := s.FlushFrame(); err != nil {
			return err
		}
		s.codes = int64(s.frames)
	}
	var k [65]byte
	copy(k[:], ch[:])
	if err := s.start(&k); err != nil {
		return err
	}
	s.raw = append(s.raw, TagCode)
	s.raw = append(s.raw, ch[:]...)
	s.raw = binary.LittleEndian.AppendUint32(s.raw, uint32(len(code)))
	s.raw = append(s.raw, code...)
	return nil
}

// Finish writes the last frame, the index (copied from idx) and the footer; it returns the
// content length.
func (s *SnapWriter) Finish() (uint64, error) {
	if err := s.FlushFrame(); err != nil {
		return 0, err
	}
	nState := s.frames
	if s.codes >= 0 {
		nState = uint64(s.codes)
	}
	indexOff := s.n
	if err := s.ib.Flush(); err != nil {
		return 0, err
	}
	c, err := io.Copy(s.w, io.NewSectionReader(s.idx, 0, int64(s.frames)*Entry))
	if err != nil {
		return 0, err
	}
	if c != int64(s.frames)*Entry {
		return 0, fmt.Errorf("era: index holds %d B, want %d", c, s.frames*Entry)
	}
	s.n += uint64(c)
	wipedOff := s.n
	var f []byte
	for _, v := range []uint64{indexOff, nState, s.frames - nState, s.block, wipedOff, 0, 0} {
		f = binary.LittleEndian.AppendUint64(f, v)
	}
	f = append(f, SnapMagic...)
	if _, err := s.w.Write(f); err != nil {
		return 0, err
	}
	s.n += uint64(len(f))
	return s.n, nil
}
