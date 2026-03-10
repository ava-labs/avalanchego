// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb/database"
)

var _ database.Reader = (*reconstructedReader)(nil)

// reconstructedReader adapts an ffi.Reconstructed to the database.Reader interface.
// It is read-only and not thread-safe (matching Reconstructed's guarantees).
type reconstructedReader struct {
	reconstructed *ffi.Reconstructed
}

// Node retrieves the value at the given path from the reconstructed view.
func (r *reconstructedReader) Node(_ common.Hash, path []byte, _ common.Hash) ([]byte, error) {
	return r.reconstructed.Get(path)
}

// newReconstructedReaderFromRevision creates a reconstructedReader by applying
// the given batch operations on top of a Revision. The caller must Drop the
// returned Reconstructed when done.
func newReconstructedReaderFromRevision(rev *ffi.Revision, ops []ffi.BatchOp) (*reconstructedReader, *ffi.Reconstructed, error) {
	recon, err := rev.Reconstruct(ops)
	if err != nil {
		return nil, nil, err
	}
	return &reconstructedReader{reconstructed: recon}, recon, nil
}
