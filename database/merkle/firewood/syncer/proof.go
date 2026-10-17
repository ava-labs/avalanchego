// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
)

var (
	_ sync.Marshaler[*RangeProof] = rangeProofMarshaler{}
	_ sync.Marshaler[struct{}]    = changeProofMarshaler{}
)

type rangeProofMarshaler struct {
	db *ffi.Database
}

func (rangeProofMarshaler) Marshal(r *RangeProof) ([]byte, error) {
	return r.rp.Marshal()
}

func (m rangeProofMarshaler) Unmarshal(data []byte) (*RangeProof, error) {
	proof, err := m.db.UnmarshalRangeProof(data)
	if err != nil {
		return nil, err
	}
	return &RangeProof{
		rp: proof,
	}, nil
}

type RangeProof struct {
	rp  *ffi.RangeProof
	vrp *ffi.VerifiedRangeProof
}

// TODO: implement an actual ChangeProof marshaler.
type changeProofMarshaler struct{}

func (changeProofMarshaler) Marshal(struct{}) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (changeProofMarshaler) Unmarshal([]byte) (struct{}, error) {
	return struct{}{}, errors.New("not implemented")
}
