package dist

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// Create makes path a fresh sparse chunk file named hash of size bytes,
// holding only its header chunks, fetched from the first source that
// serves them and checked against hash. The size must come from a trusted
// place (the manifest): the file hash does not cover it (see
// chunkfile.CreateSparse).
func Create(ctx context.Context, path string, hash Hash, size int64, srcs []Source) (*chunkfile.File, error) {
	head := make([]byte, chunkfile.HeaderLen(size))
	_, h := chunkfile.Layout(size)
	var errs error
	for _, s := range srcs {
		err := readHeader(ctx, s, hash, h, head)
		if err == nil {
			return chunkfile.CreateSparse(path, hash, size, head)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errs = errors.Join(errs, fmt.Errorf("%s: %w", s, err))
	}
	if errs == nil {
		errs = errors.New("no sources")
	}
	return nil, fmt.Errorf("dist: header of %x: %w", hash[:4], errs)
}

func readHeader(ctx context.Context, s Source, hash Hash, h int64, head []byte) error {
	for first := int64(0); first < h; {
		n := min(h-first, s.MaxChunks())
		r, err := s.Fetch(ctx, hash, first, n)
		if err != nil {
			return err
		}
		a := first * chunkfile.ChunkSize
		_, err = io.ReadFull(r, head[a:min(int64(len(head)), a+n*chunkfile.ChunkSize)])
		r.Close()
		if err != nil {
			return err
		}
		first += n
	}
	if sha256.Sum256(head) != hash {
		return chunkfile.ErrWrongHash
	}
	return nil
}
