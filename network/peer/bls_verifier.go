// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ IPVerifier = (*BLSVerifier)(nil)

	errMissingSignature      = errors.New("missing signature")
	errFailedBLSVerification = errors.New("failed bls verification")
)

// BLSVerifier verifies a signature of an ip against a BLS key
type BLSVerifier struct {
	PublicKey *bls.PublicKey
}

func (b BLSVerifier) Verify(ipBytes []byte, sig Signature) error {
	if len(sig.BLSSignature) == 0 {
		return errMissingSignature
	}

	blsSig, err := bls.SignatureFromBytes(sig.BLSSignature)
	if err != nil {
		return err
	}

	if !bls.Verify(b.PublicKey, blsSig, ipBytes) {
		return errFailedBLSVerification
	}

	return nil
}
