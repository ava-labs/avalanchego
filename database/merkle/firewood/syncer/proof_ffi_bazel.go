// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build firewood_bazel

package syncer

import "github.com/ava-labs/firewood-go-ethhash/ffi"

func marshalRangeProof(proof *ffi.RangeProof) ([]byte, error) {
	return proof.Marshal()
}

func unmarshalRangeProof(data []byte) (*ffi.RangeProof, error) {
	return ffi.UnmarshalRangeProof(data)
}

func dropRangeProof(proof *ffi.RangeProof) error {
	return proof.Drop()
}
