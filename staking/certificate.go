// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import "crypto"

type Certificate struct {
	Raw       []byte
	PublicKey crypto.PublicKey
}
