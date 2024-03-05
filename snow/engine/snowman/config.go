// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import "github.com/ava-labs/avalanchego/snow/consensus/snowball"

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	snowball.Parameters
	PartialSync bool
}
