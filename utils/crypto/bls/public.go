// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"errors"

	blst "github.com/supranational/blst/bindings/go"
)

const PublicKeyLen = blst.BLST_P1_COMPRESS_BYTES

var (
	ErrNoPublicKeys               = errors.New("no public keys")
	errFailedPublicKeyDecompress  = errors.New("couldn't decompress public key")
	errInvalidPublicKey           = errors.New("invalid public key")
	errFailedPublicKeyAggregation = errors.New("couldn't aggregate public keys")
)

type (
	PublicKey          = blst.P1Affine
	AggregatePublicKey = blst.P1Aggregate
)

// PublicKeyToBytes returns the compressed big-endian format of the public key.
func PublicKeyToBytes(pk *PublicKey) []byte {
	return pk.Compress()
}

// PublicKeyFromBytes parses the compressed big-endian format of the public key
// into a public key.
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

// AggregatePublicKeys aggregates a non-zero number of public keys into a single
// aggregated public key.
// Invariant: all [pks] have been validated.
func AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error) {
	if len(pks) == 0 {
		return nil, ErrNoPublicKeys
	}

	var agg AggregatePublicKey
	if !agg.Aggregate(pks, false) {
		return nil, errFailedPublicKeyAggregation
	}
	return agg.ToAffine(), nil
}

// Verify the [sig] of [msg] against the [pk].
// The [sig] and [pk] may have been an aggregation of other signatures and keys.
// Invariant: [pk] and [sig] have both been validated.
func Verify(pk *PublicKey, sig *Signature, msg []byte) bool {
	return sig.Verify(false, pk, false, msg, ciphersuiteSignature)
}

// Verify the possession of the secret pre-image of [sk] by verifying a [sig] of
// [msg] against the [pk].
// The [sig] and [pk] may have been an aggregation of other signatures and keys.
// Invariant: [pk] and [sig] have both been validated.
func VerifyProofOfPossession(pk *PublicKey, sig *Signature, msg []byte) bool {
	return sig.Verify(false, pk, false, msg, ciphersuiteProofOfPossession)
}
