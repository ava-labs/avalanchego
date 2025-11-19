// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"runtime"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ xsync.Marshaler[*RangeProof]  = RangeProofMarshaler{}
	_ xsync.Marshaler[*ChangeProof] = ChangeProofMarshaler{}
)

type RangeProofMarshaler struct{}

func (RangeProofMarshaler) Marshal(r *RangeProof) ([]byte, error) {
	if r == nil {
		return nil, errNilProof
	}
	if r.ffi == nil {
		return nil, nil
	}

	data, err := r.ffi.MarshalBinary()
	return data, err
}

func (RangeProofMarshaler) Unmarshal(data []byte) (*RangeProof, error) {
	proof := new(ffi.RangeProof)
	if err := proof.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return newRangeProof(proof), nil
}

type RangeProof struct {
	ffi       *ffi.RangeProof
	root      ids.ID
	maxLength int
}

// Wrap the ffi proof in our proof type.
func newRangeProof(proof *ffi.RangeProof) *RangeProof {
	return &RangeProof{
		ffi: proof,
	}
}

func (r *RangeProof) FindNextKey() (maybe.Maybe[[]byte], error) {
	// We can now get the FindNextKey iterator.
	nextKeyRange, err := r.ffi.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: this panics
	startKey := maybe.Some(nextKeyRange.StartKey())

	// Done using nextKeyRange
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return startKey, nil
}

type ChangeProofMarshaler struct{}

func (ChangeProofMarshaler) Marshal(r *ChangeProof) ([]byte, error) {
	if r == nil {
		return nil, errNilProof
	}
	if r.proof == nil {
		return nil, nil
	}

	data, err := r.proof.MarshalBinary()
	return data, err
}

func (ChangeProofMarshaler) Unmarshal(data []byte) (*ChangeProof, error) {
	proof := new(ffi.ChangeProof)
	if err := proof.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return newChangeProof(proof), nil
}

type ChangeProof struct {
	proof     *ffi.ChangeProof
	startRoot ids.ID
	endRoot   ids.ID
	startKey  maybe.Maybe[[]byte]
	maxLength int
}

// Wrap the ffi proof in our proof type.
func newChangeProof(proof *ffi.ChangeProof) *ChangeProof {
	changeProof := &ChangeProof{
		proof: proof,
	}

	// Once this struct is out of scope, free the underlying proof.
	runtime.AddCleanup(changeProof, func(ffiProof *ffi.ChangeProof) {
		if ffiProof != nil {
			_ = ffiProof.Free()
		}
	}, changeProof.proof)

	return changeProof
}

func (c *ChangeProof) FindNextKey() (maybe.Maybe[[]byte], error) {
	// We can now get the FindNextKey iterator.
	nextKeyRange, err := c.proof.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: this panics
	startKey := maybe.Some(nextKeyRange.StartKey())

	// Done using nextKeyRange
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return startKey, nil
}
