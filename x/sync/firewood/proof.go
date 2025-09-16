package firewood

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ xsync.Proof = (*rangeProof)(nil)
	_ xsync.Proof = (*changeProof)(nil)
)

type rangeProof struct {
	proof     *ffi.RangeProof
	root      ids.ID
	maxLength int
}

// Wrap the ffi proof in our proof type.
func newRangeProof(proof *ffi.RangeProof) *rangeProof {
	return &rangeProof{
		proof: proof,
	}
}

// We must only marshal the underlying proof.
// The proof is freed, making any future use of it invalid.
func (r *rangeProof) MarshalBinary() ([]byte, error) {
	if r.proof == nil {
		return nil, nil
	}

	// If the proof is non-nil, we must free it.
	data, err := r.proof.MarshalBinary()
	err = errors.Join(err, r.proof.Free())
	r.proof = nil
	return data, err
}

// The proof bytes only contains the FFI proof.
func (r *rangeProof) UnmarshalBinary(data []byte) error {
	// Free old proof if it exists.
	if r.proof != nil {
		if err := r.proof.Free(); err != nil {
			return err
		}
	}
	r.proof = new(ffi.RangeProof)
	return r.proof.UnmarshalBinary(data)
}

type changeProof struct {
	proof *ffi.ChangeProof
}

// Wrap the ffi proof in our proof type.
//
//nolint:unused
func newChangeProof(proof *ffi.ChangeProof) *changeProof {
	return &changeProof{
		proof: proof,
	}
}

// We must only marshal the underlying proof.
func (r *changeProof) MarshalBinary() ([]byte, error) {
	if r.proof == nil {
		return nil, nil
	}

	// If the proof is non-nil, we must free it.
	data, err := r.proof.MarshalBinary()
	err = errors.Join(err, r.proof.Free())
	r.proof = nil
	return data, err
}

// The proof bytes only contains the underlying proof.
func (r *changeProof) UnmarshalBinary(data []byte) error {
	// Free old proof if it exists.
	if r.proof != nil {
		if err := r.proof.Free(); err != nil {
			return err
		}
	}
	r.proof = new(ffi.ChangeProof)
	return r.proof.UnmarshalBinary(data)
}
