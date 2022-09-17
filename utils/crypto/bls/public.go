// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

const PublicKeyLen = blst.BLST_P1_COMPRESS_BYTES

var (
	errFailedPublicKeyDecompress  = errors.New("couldn't decompress public key")
	errInvalidPublicKey           = errors.New("invalid public key")
	errNoPublicKeys               = errors.New("no public keys")
	errFailedPublicKeyAggregation = errors.New("couldn't aggregate public keys")
)

type (
	PublicKey          = blst.P1Affine
	AggregatePublicKey = blst.P1Aggregate
)

func PublicKeyToBytes(pk *PublicKey) []byte {
	return pk.Compress()
}

func PublicKeyFromBytes(pkBytes []byte) (*PublicKey, error) {
	pk := new(PublicKey).Uncompress(pkBytes)
	if pk == nil {
		return nil, errFailedPublicKeyDecompress
	}
	if !pk.KeyValidate() {
		return nil, errInvalidPublicKey
	}
	return pk, nil
}

func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, errNoPublicKeys
	}

	var agg AggregatePublicKey
	if !agg.Aggregate(pks, false) {
		return nil, errFailedPublicKeyAggregation
	}
	return agg.ToAffine(), nil
}

func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	return sig.Verify(false, pk, false, msg, ciphersuite)
}
