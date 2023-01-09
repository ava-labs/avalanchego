// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ IPVerifier = (*BLSVerifier)(nil)

	errFailedBLSVerification = fmt.Errorf("failed bls verification")
)

type BLSVerifier struct {
	PublicKey *bls.PublicKey
}

func (b BLSVerifier) Verify(ipBytes []byte, sig Signature) error {
	blsSig, err := bls.SignatureFromBytes(sig.BLSSignature)
	if err != nil {
		return err
	}

	if !bls.Verify(b.PublicKey, blsSig, ipBytes) {
		return errFailedBLSVerification
	}

	return nil
}
