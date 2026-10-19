// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ sync.Marshaler[*RangeProof] = rangeProofMarshaler{}
	_ sync.Marshaler[struct{}]    = changeProofMarshaler{}
)

type rangeProofMarshaler struct{}

func (rangeProofMarshaler) Marshal(r *RangeProof) ([]byte, error) {
	bytes, err := marshalRangeProof(r.rp)
	if dropErr := dropRangeProof(r.rp); err == nil && dropErr != nil {
		return nil, dropErr
	}
	return bytes, err
}

func (rangeProofMarshaler) Unmarshal(data []byte) (*RangeProof, error) {
	proof, err := unmarshalRangeProof(data)
	if err != nil {
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
type changeProofMarshaler struct{}

func (changeProofMarshaler) Marshal(struct{}) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (changeProofMarshaler) Unmarshal([]byte) (struct{}, error) {
	return struct{}{}, errors.New("not implemented")
}
