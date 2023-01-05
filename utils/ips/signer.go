// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

// Signer cryptographically signs messages.
type Signer interface {
	// Sign signs [msg].
	Sign(msg []byte) ([]byte, error)
}
