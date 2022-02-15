// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

type PublicKey = blst.P1Affine

var errInvalidPublicKey = errors.New("invalid public key")

func PublicKeyToBytes(pk *PublicKey) []byte {
	return pk.Compress()
}

func PublicKeyFromBytes(pkBytes []byte) (*PublicKey, error) {
	pk := new(PublicKey).Uncompress(pkBytes)
	if pk == nil || !pk.KeyValidate() {
		return nil, errInvalidPublicKey
	}
	return pk, nil
}

func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, bool) {
	var agg blst.P1Aggregate
	if !agg.Aggregate(pks, false) {
		return nil, false
	}
	return agg.ToAffine(), true
}

func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	return sig.Verify(false, pk, false, msg, dst)
}
