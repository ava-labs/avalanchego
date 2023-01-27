// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import "github.com/ava-labs/avalanchego/utils/crypto/bls"

var (
	_ BLSVerifier = (*BLSKeyVerifier)(nil)
	_ BLSVerifier = (*NoKeyVerifier)(nil)
)

// BLSVerifier verifies BLS signatures for a message
type BLSVerifier interface {
	Verify(msg, sig []byte) (bool, error)
}

// BLSVerifier verifies a signature of an ip against a BLS key
type BLSKeyVerifier struct {
	PublicKey *bls.PublicKey
}

func (b BLSKeyVerifier) Verify(msg, sig []byte) (bool, error) {
	blsSig, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return false, err
	}

	if !bls.Verify(b.PublicKey, blsSig, msg) {
		return false, nil
	}

	return true, nil
}

type NoKeyVerifier struct{}

func (NoKeyVerifier) Verify(_, sig []byte) (bool, error) {
	// If there isn't a key associated, only an empty signature is considered
	// valid.
	if len(sig) == 0 {
		return true, nil
	}

	return false, nil
}
