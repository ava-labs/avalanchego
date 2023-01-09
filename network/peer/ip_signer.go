// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

type IPSigner interface {
	// Sign signs the byte representation of the unsigned ip.
	// Returns the updated Signature, and an error if signing failed.
	Sign(ipBytes []byte, sig Signature) (Signature, error)
}
