// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

type Signer interface {
	// SignBLS signs the byte representation of the unsigned ip with a bls key.
	SignBLS(ipBytes []byte) []byte
	// SignTLS signs the byte representation of the unsigned ip with a tls key.
	// Returns an error if signing failed.
	SignTLS(ipBytes []byte) ([]byte, error)
}
