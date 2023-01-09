// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import "github.com/ava-labs/avalanchego/utils/crypto/bls"

var _ IPSigner = (*BLSSigner)(nil)

// BLSSigner signs messages with a BLS key.
type BLSSigner struct {
	secretKey *bls.SecretKey
}

// NewBLSSigner returns a new instance of BLSSigner.
func NewBLSSigner(secretKey *bls.SecretKey) BLSSigner {
	return BLSSigner{
		secretKey: secretKey,
	}
}

func (b BLSSigner) Sign(ipBytes []byte, sig Signature) (Signature, error) {
	blsSig := bls.SignatureToBytes(bls.Sign(b.secretKey, ipBytes))

	sig.BLSSignature = blsSig
	return sig, nil
}
