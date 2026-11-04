// Package chunkfile implements the file contract: a file is cut into 1 MB
// chunks counted from byte 0 (last chunk short, no padding). The first H
// chunks start with the sha256 list of chunks H..N-1, data follows the list.
// The file hash is sha256 of the first H chunks. H is the smallest h >= 1 with
// 32*(N-h) <= h*ChunkSize, so it follows from the file size alone.
package chunkfile

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	ChunkSize = 1 << 20
	HashSize  = sha256.Size
)

type Hash = [HashSize]byte

// headerChunks returns H for a file of n chunks.
func headerChunks(n int64) int64 {
	return max(1, (HashSize*n+ChunkSize+HashSize-1)/(ChunkSize+HashSize))
}

// sizeLayout derives chunk count, H and header bytes from the file size.
func sizeLayout(size int64) (n, h, hdr int64) {
	n = max(1, (size+ChunkSize-1)/ChunkSize)
	h = headerChunks(n)
	return n, h, HashSize * (n - h)
}

// dataLayout finds the smallest chunk count whose header plus d data bytes
// needs exactly that many chunks. The map is monotone, so iterating from the
// lower bound reaches the least fixed point.
func dataLayout(d int64) (n, h, hdr int64) {
	n = max(1, (d+ChunkSize-1)/ChunkSize)
	for {
		h = headerChunks(n)
		hdr = HashSize * (n - h)
		n2 := max(1, (hdr+d+ChunkSize-1)/ChunkSize)
		if n2 == n {
			return n, h, hdr
		}
		n = n2
	}
}

// Writer buffers the data stream to a temp file; Finish writes the final file.
type Writer struct {
	path string
	tmp  *os.File
	n    int64
}

// Create starts a chunk file at path. Data goes to path+".data" until Finish.
func Create(path string) (*Writer, error) {
	tmp, err := os.Create(path + ".data")
	if err != nil {
		return nil, err
	}
	NoPrealloc(tmp)
	return &Writer{path: path, tmp: tmp}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	w.n += int64(n)
	return n, err
}

// Finish writes the final file (header then data), fsyncs it, renames it into
// place and returns its hash. The temp files are removed on any outcome.
func (w *Writer) Finish() (Hash, error) {
	defer os.Remove(w.tmp.Name())
	defer w.tmp.Close()
	part := w.path + ".part"
	h, err := w.finish(part)
	if err != nil {
		os.Remove(part)
		return Hash{}, err
	}
	return h, os.Rename(part, w.path)
}

func (w *Writer) finish(part string) (Hash, error) {
	d := w.n
	n, h, hdr := dataLayout(d)
	size := hdr + d
	out, err := os.Create(part)
	if err != nil {
		return Hash{}, err
	}
	defer out.Close()
	if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
		return Hash{}, err
	}
	buf := make([]byte, ChunkSize)
	// Data that shares the last header chunk with the list.
	first := min(d, h*ChunkSize-hdr)
	if _, err := io.ReadFull(w.tmp, buf[:first]); err != nil {
		return Hash{}, err
	}
	if _, err := out.WriteAt(buf[:first], hdr); err != nil {
		return Hash{}, err
	}
	list := make([]byte, 0, hdr)
	for i := h; i < n; i++ {
		l := min(ChunkSize, size-i*ChunkSize)
		if _, err := io.ReadFull(w.tmp, buf[:l]); err != nil {
			return Hash{}, err
		}
		sum := sha256.Sum256(buf[:l])
		list = append(list, sum[:]...)
		if _, err := out.WriteAt(buf[:l], i*ChunkSize); err != nil {
			return Hash{}, err
		}
	}
	if _, err := out.WriteAt(list, 0); err != nil {
		return Hash{}, err
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(out, 0, min(size, h*ChunkSize))); err != nil {
		return Hash{}, err
	}
	if err := out.Sync(); err != nil {
		return Hash{}, err
	}
	return Hash(sum.Sum(nil)), out.Close()
}

// AbsentError is returned when a read touches a chunk that is not on disk.
type AbsentError struct{ Chunk int64 }

func (e *AbsentError) Error() string { return fmt.Sprintf("chunkfile: chunk %d absent", e.Chunk) }

var (
	ErrInUse     = errors.New("chunkfile: chunk in use")
	ErrHeader    = errors.New("chunkfile: header chunk")
	ErrMismatch  = errors.New("chunkfile: chunk hash mismatch")
	ErrWrongHash = errors.New("chunkfile: file hash mismatch")
)

var epoch = time.Now()

// File is an open chunk file. Presence, read stamps and refcounts live in
// memory only and are rebuilt at Open.
//
// Presence comes from the filesystem (SEEK_DATA) and is trusted. The one
// way a chunk can be present with wrong bytes is a crash while it was being
// written; Write journals every run before its first byte (see journal.go)
// and Open hashes the chunks of the runs a crash left open, punching any
// that do not match. Files made by Create are complete by construction.
// Open each path once per process and share the File: the journal and the
// presence bitmap belong to it.
type File struct {
	f         *os.File
	n, h, hdr int64
	size      int64
	hashes    []byte // hashes of chunks h..n-1
	present   []atomic.Uint64
	stamps    []atomic.Uint32
	refs      []atomic.Int32
	mu        sync.Mutex // serialises Write, Punch and punching a torn chunk
	j         *journal

	// What Open found in the journal: chunks hashed, and of those punched.
	Suspects, Punched int64
}

var chunkPool = sync.Pool{New: func() any { b := make([]byte, ChunkSize); return &b }}

// Open opens a (possibly sparse) chunk file and checks its header against
// want. The header chunks must be present.
func Open(path string, want Hash) (*File, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	cf, err := open(f, want)
	if err == nil {
		err = cf.recover(path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	cf.j = newJournal(path, f)
	return cf, nil
}

func open(f *os.File, want Hash) (*File, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	n, h, hdr := sizeLayout(size)
	cf := &File{
		f: f, n: n, h: h, hdr: hdr, size: size,
		present: make([]atomic.Uint64, (n+63)/64),
		stamps:  make([]atomic.Uint32, n),
		refs:    make([]atomic.Int32, n),
	}
	if err := cf.scan(); err != nil {
		return nil, err
	}
	for i := range h {
		if !cf.Present(i) {
			return nil, &AbsentError{Chunk: i}
		}
	}
	head := make([]byte, min(size, h*ChunkSize))
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, err
	}
	if sha256.Sum256(head) != want {
		return nil, ErrWrongHash
	}
	cf.hashes = bytes.Clone(head[:hdr]) // not the whole header chunk: a tier0 file would hold 1 MB for its 4 KB hash list
	return cf, nil
}

// scan rebuilds the presence bitmap: a chunk is present when its whole range
// is data (a crash mid-write or a punched short tail leaves it absent).
func (cf *File) scan() error {
	for off := int64(0); off < cf.size; {
		d, err := cf.f.Seek(off, seekData)
		if errors.Is(err, syscall.ENXIO) {
			return nil
		}
		if err != nil {
			return err
		}
		e, err := cf.f.Seek(d, seekHole)
		if err != nil {
			return err
		}
		for i := (d + ChunkSize - 1) / ChunkSize; i < cf.n; i++ {
			s, end := cf.Chunk(i)
			if s < d || end > e {
				break
			}
			cf.present[i/64].Or(1 << (i % 64))
		}
		off = e
	}
	return nil
}

// Close checkpoints the journal (fsyncing written chunks) and closes.
func (cf *File) Close() error {
	var err error
	if cf.j != nil {
		err = cf.j.close()
	}
	return errors.Join(err, cf.f.Close())
}

// NumChunks is N, HeaderChunks is H, DataSize is the length of the data part.
func (cf *File) NumChunks() int64    { return cf.n }
func (cf *File) HeaderChunks() int64 { return cf.h }
func (cf *File) DataSize() int64     { return cf.size - cf.hdr }

// Chunk returns the byte range [start, end) of chunk i in the file.
func (cf *File) Chunk(i int64) (start, end int64) {
	return i * ChunkSize, min((i+1)*ChunkSize, cf.size)
}

func (cf *File) Present(i int64) bool { return cf.present[i/64].Load()&(1<<(i%64)) != 0 }

// Stamp is the last-read time of chunk i in seconds since process start
// (0 = not read since Open).
func (cf *File) Stamp(i int64) uint32 { return cf.stamps[i].Load() }

// check hashes present chunk i; the caller holds a ref. A mismatch punches
// the chunk and returns AbsentError. Refs do not stop that punch: bytes
// that do not match their hash must not be read.
func (cf *File) check(i int64) error {
	bp := chunkPool.Get().(*[]byte)
	defer chunkPool.Put(bp)
	s, e := cf.Chunk(i)
	b := (*bp)[:e-s]
	if _, err := cf.f.ReadAt(b, s); err != nil {
		return err
	}
	bit := uint64(1) << (i % 64)
	if i < cf.h || cf.Verify(i, b) == nil {
		return nil
	}
	cf.mu.Lock()
	defer cf.mu.Unlock()
	// A Write may have replaced the bytes while we hashed the old ones.
	if _, err := cf.f.ReadAt(b, s); err != nil {
		return err
	}
	if cf.Verify(i, b) == nil {
		return nil
	}
	cf.present[i/64].And(^bit)
	if err := punch(cf.f, s, e-s); err != nil {
		return err
	}
	return &AbsentError{Chunk: i}
}

// Check hashes chunk i now (every call) and returns nil if it matches, or
// AbsentError if it is absent or did not match (it is punched then). Reads
// do not hash: presence is trusted (see File). Check is for an explicit
// scrub.
func (cf *File) Check(i int64) error {
	if err := cf.Acquire(i); err != nil {
		return err
	}
	defer cf.Release(i)
	return cf.check(i)
}

// Acquire pins a present chunk so Punch skips it until Release.
func (cf *File) Acquire(i int64) error {
	cf.refs[i].Add(1)
	if !cf.Present(i) {
		cf.refs[i].Add(-1)
		return &AbsentError{Chunk: i}
	}
	return nil
}

func (cf *File) Release(i int64) { cf.refs[i].Add(-1) }

// ReadAt reads the data part, offsets as if the header did not exist. A
// read touching an absent chunk returns AbsentError.
func (cf *File) ReadAt(p []byte, off int64) (int, error) {
	d := cf.DataSize()
	if off < 0 {
		return 0, errors.New("chunkfile: negative offset")
	}
	end := min(off+int64(len(p)), d)
	if end <= off {
		return 0, io.EOF
	}
	first, last := (off+cf.hdr)/ChunkSize, (end+cf.hdr-1)/ChunkSize
	for i := first; i <= last; i++ {
		if err := cf.Acquire(i); err != nil {
			for j := first; j < i; j++ {
				cf.Release(j)
			}
			return 0, err
		}
	}
	now := uint32(time.Since(epoch)/time.Second) + 1
	for i := first; i <= last; i++ {
		cf.stamps[i].Store(now)
	}
	n, err := cf.f.ReadAt(p[:end-off], off+cf.hdr)
	for i := first; i <= last; i++ {
		cf.Release(i)
	}
	if err == nil && end < off+int64(len(p)) {
		err = io.EOF
	}
	return n, err
}

// Verify checks b against the hash list entry of data chunk i.
func (cf *File) Verify(i int64, b []byte) error {
	if i < cf.h || i >= cf.n {
		return ErrHeader
	}
	s, e := cf.Chunk(i)
	sum := sha256.Sum256(b)
	if int64(len(b)) != e-s || !bytes.Equal(sum[:], cf.hashes[(i-cf.h)*HashSize:][:HashSize]) {
		return ErrMismatch
	}
	return nil
}

// Write verifies b, stores it at chunk i and marks the chunk present. A
// chunk outside every run begun with BeginRun is journaled on its own (one
// journal fsync per chunk); writers of many chunks call BeginRun first.
func (cf *File) Write(i int64, b []byte) error {
	if err := cf.Verify(i, b); err != nil {
		return err
	}
	if !cf.j.covers(i) {
		end, err := cf.BeginRun(i, 1)
		if err != nil {
			return err
		}
		defer end()
	}
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if _, err := cf.f.WriteAt(b, i*ChunkSize); err != nil {
		return err
	}
	cf.present[i/64].Or(1 << (i % 64))
	return nil
}

// BeginRun journals chunks [first, first+n) as being written (one fsync of
// the journal) and returns end, to call once their Writes are done. A crash
// before the next checkpoint after end makes Open hash those chunks.
func (cf *File) BeginRun(first, n int64) (end func() error, err error) {
	r := &run{first, n}
	if err := cf.j.begin(r); err != nil {
		return nil, err
	}
	return func() error { return cf.j.end(r) }, nil
}

// Sync checkpoints now: written chunks are fsynced and leave the journal.
func (cf *File) Sync() error { return cf.j.checkpoint() }

// Punch frees data chunk i on disk and marks it absent. A pinned chunk is
// left alone and ErrInUse returned.
func (cf *File) Punch(i int64) error {
	if i < cf.h || i >= cf.n {
		return ErrHeader
	}
	cf.mu.Lock()
	defer cf.mu.Unlock()
	bit := uint64(1) << (i % 64)
	if cf.present[i/64].And(^bit)&bit == 0 {
		return nil
	}
	// Clear first, then look at refs: a concurrent Acquire either sees the
	// bit gone or is seen here (atomics are sequentially consistent).
	if cf.refs[i].Load() > 0 {
		cf.present[i/64].Or(bit)
		return ErrInUse
	}
	s, e := cf.Chunk(i)
	return punch(cf.f, s, e-s)
}
