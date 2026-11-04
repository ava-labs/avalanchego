package chunkfile

import (
	"crypto/sha256"
	"errors"
	"io"
)

// Sums writes a stream to w and hashes it in ChunkSize pieces on its own goroutine as it
// goes: the hash list SealWith takes, so a seal does not read the data back. Close it once.
type Sums struct {
	w      io.Writer
	cur    []byte // the open piece
	sums   []Hash
	ch     chan []byte
	free   chan []byte
	done   chan struct{}
	err    error // a Write that failed: the sums no longer follow the file
	closed bool
}

const sumBufs = 8 // pieces in flight to the hashing goroutine

// NewSums hashes what is written to w through it, after the data already in w: prefix (its
// first n bytes, read once here, in pieces) is hashed first. Sums does not implement
// io.ReaderFrom, so a bufio.Writer over it passes every byte through Write.
func NewSums(w io.Writer, prefix io.ReaderAt, n int64) (*Sums, error) {
	s := &Sums{w: w, ch: make(chan []byte, sumBufs), free: make(chan []byte, sumBufs+1), done: make(chan struct{})}
	for range sumBufs + 1 {
		s.free <- make([]byte, 0, ChunkSize)
	}
	s.cur = <-s.free
	go func() {
		defer close(s.done)
		for b := range s.ch {
			s.sums = append(s.sums, sha256.Sum256(b))
			s.free <- b[:0]
		}
	}()
	for off := int64(0); off < n; {
		k := min(n-off, int64(cap(s.cur)-len(s.cur)))
		b := s.cur[len(s.cur) : int64(len(s.cur))+k]
		if _, err := prefix.ReadAt(b, off); err != nil {
			s.Close()
			return nil, err
		}
		s.add(b)
		off += k
	}
	return s, nil
}

// add takes p into the open piece (p may alias its free room) and hands full pieces on.
func (s *Sums) add(p []byte) {
	for len(p) > 0 {
		k := copy(s.cur[len(s.cur):cap(s.cur)], p)
		s.cur = s.cur[:len(s.cur)+k]
		p = p[k:]
		if len(s.cur) == cap(s.cur) {
			s.ch <- s.cur
			s.cur = <-s.free
		}
	}
}

func (s *Sums) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	s.add(p[:n])
	if err != nil && s.err == nil {
		s.err = err
	}
	return n, err
}

var errSumsWrite = errors.New("chunkfile: a write under Sums failed")

// Close hashes the last (short) piece and returns the hash of every piece in order. After a
// failed Write it returns an error and no sums.
func (s *Sums) Close() ([]Hash, error) {
	if s.closed {
		return nil, errors.New("chunkfile: Sums closed twice")
	}
	s.closed = true
	if len(s.cur) > 0 {
		s.ch <- s.cur
		s.cur = nil
	}
	close(s.ch)
	<-s.done
	if s.err != nil {
		return nil, errors.Join(errSumsWrite, s.err)
	}
	return s.sums, nil
}
