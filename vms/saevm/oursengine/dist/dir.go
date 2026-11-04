package dist

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// Dir is the registry of a directory's chunk files: one open
// *chunkfile.File per hash, shared by readers, the server and downloaders,
// so all of them see one presence bitmap. (Two Files over one path would
// each keep their own bitmap: chunks one writes stay absent in the other.)
//
// A chunk file is named by its hash in hex, alone or with any extension
// ("<hex>", "<hex>.tier0"); a name is accepted only when the file's header
// hashes to it, so other files sharing the prefix (".facts") are skipped.
// Temp names ending in ".part" or ".data" are ignored.
type Dir struct {
	path string
	mu   sync.Mutex
	cand map[Hash][]string
	open map[Hash]*chunkfile.File
}

// OpenDir lists path's candidate chunk files; files open on first use.
func OpenDir(path string) (*Dir, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	d := &Dir{path: path, cand: map[Hash][]string{}, open: map[Hash]*chunkfile.File{}}
	for _, e := range ents {
		if h, ok := parseName(e.Name()); ok && e.Type().IsRegular() {
			d.cand[h] = append(d.cand[h], filepath.Join(path, e.Name()))
		}
	}
	return d, nil
}

func parseName(name string) (Hash, bool) {
	var h Hash
	if len(name) < 2*len(h) || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".data") {
		return h, false
	}
	if len(name) > 2*len(h) && name[2*len(h)] != '.' {
		return h, false
	}
	_, err := hex.Decode(h[:], []byte(name[:2*len(h)]))
	return h, err == nil
}

// Path is the directory.
func (d *Dir) Path() string { return d.path }

// File returns the open chunk file named hash, opening it on first use.
func (d *Dir) File(hash Hash) (*chunkfile.File, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if f := d.open[hash]; f != nil {
		return f, nil
	}
	for _, p := range d.cand[hash] {
		f, err := chunkfile.Open(p, hash)
		var ae *chunkfile.AbsentError
		if errors.Is(err, chunkfile.ErrWrongHash) || errors.As(err, &ae) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		d.open[hash] = f
		return f, nil
	}
	return nil, ErrUnknown
}

// Add registers a file opened or created elsewhere (a download target).
func (d *Dir) Add(hash Hash, f *chunkfile.File) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if g := d.open[hash]; g != nil && g != f {
		return fmt.Errorf("dist: %x already open", hash[:4])
	}
	d.open[hash] = f
	return nil
}

// Remove closes chunk file hash and deletes it (path, with its journal): the directory no
// longer holds it, and a later download creates it again (Add). Readers must be done with it
// (an eviction makes the file unread first; a pinned chunk would have kept it).
func (d *Dir) Remove(hash Hash, path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if f := d.open[hash]; f != nil {
		err = f.Close()
		delete(d.open, hash)
	}
	d.cand[hash] = slices.DeleteFunc(d.cand[hash], func(p string) bool { return p == path })
	if len(d.cand[hash]) == 0 {
		delete(d.cand, hash)
	}
	for _, p := range []string{path + ".wal", path} {
		if e := os.Remove(p); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}
	return err
}

// Dirs serves the chunk files of several directories as one (a node keeps
// its snapshot and its tier0 files apart): File asks each in turn.
type Dirs []*Dir

func (ds Dirs) File(hash Hash) (*chunkfile.File, error) {
	for _, d := range ds {
		f, err := d.File(hash)
		if !errors.Is(err, ErrUnknown) {
			return f, err
		}
	}
	return nil, ErrUnknown
}

// Hashes lists the files of every directory (open or not), each once.
func (ds Dirs) Hashes() []Hash {
	seen := map[Hash]bool{}
	var hs []Hash
	for _, d := range ds {
		for _, h := range d.Hashes() {
			if !seen[h] {
				seen[h] = true
				hs = append(hs, h)
			}
		}
	}
	return hs
}

// Hashes lists the files the directory holds (open or not).
func (d *Dir) Hashes() []Hash {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := map[Hash]bool{}
	var hs []Hash
	for h := range d.open {
		seen[h] = true
		hs = append(hs, h)
	}
	for h := range d.cand {
		if !seen[h] {
			hs = append(hs, h)
		}
	}
	return hs
}

// Close closes every open file.
func (d *Dir) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	for h, f := range d.open {
		err = errors.Join(err, f.Close())
		delete(d.open, h)
	}
	return err
}

// DirSource serves chunks from the chunk files of a local Dir: another data
// directory, a disk the operator plugged in, tests.
type DirSource struct {
	D   *Dir
	Cap int64
}

func NewDirSource(d *Dir) *DirSource  { return &DirSource{D: d, Cap: HTTPCap} }
func (s *DirSource) String() string   { return "dir:" + s.D.path }
func (s *DirSource) MaxChunks() int64 { return s.Cap }

func (s *DirSource) Fetch(ctx context.Context, hash Hash, first, n int64) (io.ReadCloser, error) {
	f, err := s.D.File(hash)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > s.Cap || first < 0 || first >= f.NumChunks() {
		return nil, fmt.Errorf("dist: fetch %d+%d of %d chunks, cap %d", first, n, f.NumChunks(), s.Cap)
	}
	n = min(n, f.NumChunks()-first)
	for i := first; i < first+n; i++ {
		if !f.Present(i) {
			return nil, ErrNotHeld
		}
	}
	return &chunkReader{ctx: ctx, f: f, next: first, end: first + n}, nil
}

func (s *DirSource) Have(ctx context.Context, hash Hash, n int64) (*Bitmap, error) {
	f, err := s.D.File(hash)
	if err != nil {
		return nil, err
	}
	if f.NumChunks() != n {
		return nil, fmt.Errorf("dist: %x has %d chunks, asked for %d", hash[:4], f.NumChunks(), n)
	}
	return Presence(f), nil
}

// chunkReader streams chunks [next, end) of f through ReadChunk (each one
// present; presence is trusted, see chunkfile.File).
type chunkReader struct {
	ctx       context.Context
	f         *chunkfile.File
	next, end int64
	buf       []byte
	pending   []byte
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.next >= r.end {
			return 0, io.EOF
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if r.buf == nil {
			r.buf = make([]byte, chunkfile.ChunkSize)
		}
		k, err := r.f.ReadChunk(r.next, r.buf)
		if err != nil {
			return 0, err
		}
		r.next++
		r.pending = r.buf[:k]
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *chunkReader) Close() error { return nil }
