// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type VM interface {
	block.ChainVM

	// GetPreference returns the ID of the currently preferred block.
	// If no blocks have been accepted/preferred by consensus yet, it is
	// assumed there is a definitionally accepted block, the Genesis block, that
	// will be returned.
	GetPreference() ids.ID
}
