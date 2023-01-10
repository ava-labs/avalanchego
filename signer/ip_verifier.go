// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

type Verifier interface {
	// VerifyTLS [sig] against [msg] using a tls key.
	// Returns an error if verification fails.
	VerifyTLS(ipBytes []byte, sig []byte) error
	// VerifyBLS [sig] against [msg] using a bls key.
	// Returns an error if verification fails.
	VerifyBLS(ipBytes []byte, sig []byte) error
}
