package dist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

type Hash = chunkfile.Hash

// Source is anything that holds chunks: a node's HTTP endpoint, the R2
// mirror, a local directory, later a p2p peer.
type Source interface {
	// Fetch returns the raw file bytes of chunks [first, first+n) of the
	// file named hash (header chunks included, chunk i = bytes
	// [i MB, (i+1) MB)): n MB, less when the range reaches the end of the
	// file. n is at most MaxChunks. ErrUnknown: the source lacks the file;
	// ErrNotHeld: it lacks a chunk of the range.
	Fetch(ctx context.Context, hash Hash, first, n int64) (io.ReadCloser, error)
	// Have returns the chunks the source announces for a file of n chunks.
	// ErrNoAnnounce: the source does not announce (a mirror holds whole
	// files and has every chunk of the files it has).
	Have(ctx context.Context, hash Hash, n int64) (*Bitmap, error)
	// MaxChunks is the most chunks one Fetch asks for.
	MaxChunks() int64
	String() string
}

var (
	ErrUnknown    = errors.New("dist: file unknown to source")
	ErrNotHeld    = errors.New("dist: chunk not held by source")
	ErrNoAnnounce = errors.New("dist: source does not announce")
)

// WriteRun reads chunks [first, first+n) of f's file from r and writes each
// one (chunkfile.Write verifies it against the hash list) under one journal
// run (chunkfile.BeginRun: one fsync per run); a chunk already present is
// read past, not written. done is called after each
// chunk with wrote = false for those. It returns the chunks consumed; on a
// bad chunk it stops with chunkfile.ErrMismatch and that chunk stays absent.
func WriteRun(f *chunkfile.File, r io.Reader, first, n int64, buf []byte, done func(i int64, wrote bool)) (got int64, err error) {
	end, err := f.BeginRun(first, n)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, end()) }()
	for i := first; i < first+n; i++ {
		s, e := f.Chunk(i)
		b := buf[:e-s]
		if _, err := io.ReadFull(r, b); err != nil {
			return i - first, fmt.Errorf("dist: chunk %d: %w", i, err)
		}
		wrote := !f.Present(i)
		if wrote {
			if err := f.Write(i, b); err != nil {
				return i - first, fmt.Errorf("dist: chunk %d: %w", i, err)
			}
		}
		if done != nil {
			done(i, wrote)
		}
	}
	return n, nil
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, chunkfile.ChunkSize); return &b }}
