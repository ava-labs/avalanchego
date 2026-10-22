// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !firewood_bazel

package syncer

import "github.com/ava-labs/firewood-go-ethhash/ffi"

func marshalRangeProof(proof *ffi.RangeProof) ([]byte, error) {
	return proof.MarshalBinary()
}

func unmarshalRangeProof(data []byte) (*ffi.RangeProof, error) {
	proof := new(ffi.RangeProof)
	if err := proof.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return proof, nil
}

func dropRangeProof(proof *ffi.RangeProof) error {
	return proof.Free()
}
