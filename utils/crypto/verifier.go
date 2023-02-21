// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

// MultiVerifier supports the verification of multiple signature types
type MultiVerifier interface {
	// VerifyTLS [sig] against [msg] using a tls key.
	// Returns an error if verification fails.
	VerifyTLS(msg, sig []byte) error
	// VerifyBLS [sig] against [msg] using a bls key.
	// Returns if [sig] is valid for [msg]
	// Returns an error if verification fails.
	VerifyBLS(msg, sig []byte) (bool, error)
}
