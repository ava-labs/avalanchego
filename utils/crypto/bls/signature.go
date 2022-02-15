// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

type Signature = blst.P2Affine

var errInvalidSignature = errors.New("invalid signature")

func SignatureToBytes(sig *Signature) []byte {
	return sig.Compress()
}

func SignatureFromBytes(sigBytes []byte) (*Signature, error) {
	sig := new(Signature).Uncompress(sigBytes)
	if sig == nil || !sig.SigValidate(false) {
		return nil, errInvalidSignature
	}
	return sig, nil
}

func AggregateSignatures(pks []*Signature) (*Signature, bool) {
	var agg blst.P2Aggregate
	if !agg.Aggregate(pks, false) {
		return nil, false
	}
	return agg.ToAffine(), true
}
