// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ sync.Marshaler[*RangeProof] = RangeProofMarshaler{}
	_ sync.Marshaler[struct{}]    = ChangeProofMarshaler{}
)

type RangeProofMarshaler struct{}

func (RangeProofMarshaler) Marshal(r *RangeProof) ([]byte, error) {
	return r.rp.MarshalBinary()
}

func (RangeProofMarshaler) Unmarshal(data []byte) (*RangeProof, error) {
	proof := new(ffi.RangeProof)
	if err := proof.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return &RangeProof{
		rp: proof,
	}, nil
}

type RangeProof struct {
	rp        *ffi.RangeProof
	root      ids.ID
	maxLength int
}

// TODO: implement an actual ChangeProof marshaler.
type ChangeProofMarshaler struct{}

func (ChangeProofMarshaler) Marshal(struct{}) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (ChangeProofMarshaler) Unmarshal([]byte) (struct{}, error) {
	return struct{}{}, errors.New("not implemented")
}
