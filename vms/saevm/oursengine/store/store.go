// Package store is the node's state read path: the snapshot base (era pack ERASNAP2 content
// stored as a chunkfile, read through an mmap, every chunk verified against the hash list
// before its first touch) under an overlay of every write since the base (rebuilt from the
// cut tier0 files at start), served to the executor as one view.
package store

import (
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// WrapBase writes the content of the era base object src (casfs trailer dropped) as the
// chunkfile dst and returns its hash: the snapshot hash the manifest lists. Done once per base.
func WrapBase(src, dst string) (chunkfile.Hash, error) {
	b, done, err := era.Content(src)
	if err != nil {
		return chunkfile.Hash{}, err
	}
	defer done()
	if _, err := era.OpenSnap(b); err != nil {
		return chunkfile.Hash{}, fmt.Errorf("store: %s: %w", src, err)
	}
	w, err := chunkfile.Create(dst)
	if err != nil {
		return chunkfile.Hash{}, err
	}
	for rest := b; len(rest) > 0; {
		n := min(len(rest), 64<<20)
		if _, err := w.Write(rest[:n]); err != nil {
			return chunkfile.Hash{}, err
		}
		rest = rest[n:]
	}
	return w.Finish()
}

// BaseContent is the file offset of the base content in m, the mapped base file whose chunk
// file is cf. The content runs to the end of the file; it starts right after the hash list in
// a base written by Create and Finish (WrapBase, the genesis base) or by a seal from before
// chunkfile.SealLayout, and at the first chunk after the list, zeros between, in a base
// sealed since. The era magic at the start of the data part tells them apart (a list that
// fills its chunks exactly has no zeros after it: both layouts start the content there).
func BaseContent(cf *chunkfile.File, m []byte) (uint64, error) {
	hdr := cf.Size() - cf.DataSize()
	at := min(cf.HeaderChunks()*chunkfile.ChunkSize, cf.Size()) // in the header chunks: present
	if hdr == at || hdr+8 <= at && string(m[hdr:hdr+8]) == era.SnapMagic {
		return uint64(hdr), nil
	}
	for _, c := range m[hdr:at] {
		if c != 0 {
			return 0, fmt.Errorf("store: base data part starts neither with %s nor with zeros to its first chunk", era.SnapMagic)
		}
	}
	return uint64(at), nil
}

// OpenBase maps the base chunkfile at path whose open handle is cf (the shared handle of its
// dist.Dir, so chunks a download fills are seen; OpenBase does not close it). A byte range is
// read only when the chunks covering it are present (chunkfile's crash journal makes presence
// trustworthy); an absent chunk (a hole in a base still downloading) fails the read with
// *chunkfile.AbsentError instead of reading zeros.
func OpenBase(cf *chunkfile.File, path string) (*base.Base, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.Size() != cf.Size() {
		f.Close()
		return nil, fmt.Errorf("store: %s is %d B, its chunk file %d B", path, st.Size(), cf.Size())
	}
	m, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	f.Close()
	if err != nil {
		return nil, err
	}
	hdr, err := BaseContent(cf, m)
	if err != nil {
		syscall.Munmap(m)
		return nil, err
	}
	check := func(off, n uint64) error {
		if n == 0 {
			return nil
		}
		for c := (hdr + off) / chunkfile.ChunkSize; c <= (hdr+off+n-1)/chunkfile.ChunkSize; c++ {
			if !cf.Present(int64(c)) {
				return &chunkfile.AbsentError{Chunk: int64(c)}
			}
		}
		return nil
	}
	var once sync.Once
	var uerr error
	done := func() error { once.Do(func() { uerr = syscall.Munmap(m) }); return uerr }
	b, err := base.OpenBytes(m[hdr:], check, done)
	if err != nil {
		done()
		return nil, err
	}
	return b, nil
}
