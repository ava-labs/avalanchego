// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

type IPVerifier interface {
	// Verify [signature] against [message]. Returns an error if verification
	// fails.
	Verify(ipBytes []byte, sig Signature) error
}
