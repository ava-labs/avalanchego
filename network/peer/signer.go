// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import "fmt"

// Signer cryptographically signs messages.
type Signer interface {
	fmt.Stringer
	Sign(msg []byte) ([]byte, error)
}
