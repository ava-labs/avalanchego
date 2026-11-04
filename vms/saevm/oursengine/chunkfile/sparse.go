package chunkfile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"
)

// Layout returns the chunk count N and header chunk count H of a file of
// size bytes.
func Layout(size int64) (n, h int64) {
	n, h, _ = sizeLayout(size)
	return n, h
}

// HeaderLen is the byte length of the first H chunks of a file of size
// bytes: what a fresh node fetches first and checks against the file hash.
func HeaderLen(size int64) int64 {
	_, h, _ := sizeLayout(size)
	return min(size, h*ChunkSize)
}

// CreateSparse creates the chunk file named want at path as a sparse file of
// size bytes that holds only its header chunks; every data chunk is absent
// until Write. header is the first HeaderLen(size) bytes and must hash to
// want. It fails if path exists.
//
// The size is not covered by the file hash: cutting a file at a chunk
// boundary that keeps H leaves the header bytes, and so the hash, unchanged,
// and every remaining chunk still matches its list entry. The size must come
// from a trusted place (the manifest), never from a peer.
func CreateSparse(path string, want Hash, size int64, header []byte) (*File, error) {
	if size <= 0 {
		return nil, fmt.Errorf("chunkfile: size %d", size)
	}
	if int64(len(header)) != HeaderLen(size) {
		return nil, fmt.Errorf("chunkfile: header is %d bytes, size %d needs %d", len(header), size, HeaderLen(size))
	}
	if sha256.Sum256(header) != want {
		return nil, ErrWrongHash
	}
	part := fmt.Sprintf("%s.%d.part", path, time.Now().UnixNano())
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	defer os.Remove(part)
	err = func() error {
		if err := f.Truncate(size); err != nil {
			return err
		}
		if _, err := f.WriteAt(header, 0); err != nil {
			return err
		}
		// APFS fills holes of a truncated file when writes land between
		// them: truncate 200 MB, write every third 1 MB chunk, close, and
		// SEEK_DATA reports all 200 MB as data, allocated (16 MB files are
		// allocated on Truncate already). One punch over the whole data part
		// does not stop it; a punch per chunk does: the file then holds
		// exactly the chunks written, across closes and reopens (measured
		// 2026-10-22, macOS 27). On Linux the punches find holes and cost
		// nothing.
		n, h, _ := sizeLayout(size)
		for i := h; i < n; i++ {
			s, e := i*ChunkSize, min((i+1)*ChunkSize, size)
			if err := punch(f, s, e-s); err != nil {
				return err
			}
		}
		if err := f.Sync(); err != nil {
			return err
		}
		return os.Link(part, path) // fails if path exists
	}()
	if err != nil {
		f.Close()
		return nil, err
	}
	cf, err := open(f, want)
	if err != nil {
		f.Close()
		return nil, err
	}
	cf.j = newJournal(path, f)
	return cf, nil
}

// ErrShort is returned by ReadChunk when p is shorter than the chunk.
var ErrShort = errors.New("chunkfile: buffer shorter than chunk")

// ReadChunk reads the raw bytes of chunk i (header chunks included) into p
// and returns the chunk length; it stamps the chunk, and an absent chunk
// returns AbsentError. It serves other nodes the file's own bytes.
func (cf *File) ReadChunk(i int64, p []byte) (int, error) {
	if i < 0 || i >= cf.n {
		return 0, fmt.Errorf("chunkfile: chunk %d out of %d", i, cf.n)
	}
	s, e := cf.Chunk(i)
	if int64(len(p)) < e-s {
		return 0, ErrShort
	}
	if err := cf.Acquire(i); err != nil {
		return 0, err
	}
	defer cf.Release(i)
	cf.stamps[i].Store(uint32(time.Since(epoch)/time.Second) + 1)
	return cf.f.ReadAt(p[:e-s], s)
}

// Size is the file length in bytes.
func (cf *File) Size() int64 { return cf.size }

// Verified is Present: Open settles the chunks a crash could have left
// wrong (see File), so every present chunk is trusted.
func (cf *File) Verified(i int64) bool { return cf.Present(i) }

// PunchRange frees [off, off+n) of any file, keeping its size (fallocate
// PUNCH_HOLE on Linux, F_PUNCHHOLE on macOS; APFS needs off and n aligned to
// its block size). For writers that free a temp file behind a copy.
func PunchRange(f *os.File, off, n int64) error { return punch(f, off, n) }
