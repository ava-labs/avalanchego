// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

const SignatureLen = blst.BLST_P2_COMPRESS_BYTES

var (
	ErrFailedSignatureDecompress  = errors.New("couldn't decompress signature")
	ErrInvalidSignature           = errors.New("invalid signature")
	ErrNoSignatures               = errors.New("no signatures")
	ErrFailedSignatureAggregation = errors.New("couldn't aggregate signatures")
)

type (
	Signature          = blst.P2Affine
	AggregateSignature = blst.P2Aggregate
)

// SignatureToBytes returns the compressed big-endian format of the signature.
func SignatureToBytes(sig *Signature) []byte {
	return sig.Compress()
}

// SignatureFromBytes parses the compressed big-endian format of the signature
// into a signature.
func SignatureFromBytes(sigBytes []byte) (*Signature, error) {
	sig := new(Signature).Uncompress(sigBytes)
	if sig == nil {
		return nil, ErrFailedSignatureDecompress
	}
	if !sig.SigValidate(false) {
		return nil, ErrInvalidSignature
	}
	return sig, nil
}

// AggregateSignatures aggregates a non-zero number of signatures into a single
// aggregated signature.
// Invariant: all [sigs] have been validated.
func AggregateSignatures(sigs []*Signature) (*Signature, error) {
	if len(sigs) == 0 {
		return nil, ErrNoSignatures
	}

	var agg AggregateSignature
	if !agg.Aggregate(sigs, false) {
		return nil, ErrFailedSignatureAggregation
	}
	return agg.ToAffine(), nil
}
