// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"

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

type ChangeProofMarshaler struct{}

func (ChangeProofMarshaler) Marshal(*ChangeProof) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (ChangeProofMarshaler) Unmarshal([]byte) (*ChangeProof, error) {
	return nil, errors.New("not implemented")
}

type ChangeProof struct{}
