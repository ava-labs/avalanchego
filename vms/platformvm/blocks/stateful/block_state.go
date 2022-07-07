// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

// TODO rename to reflect the fact that this is from/to the database?
type statelessBlockState interface {
	GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error)
	AddStatelessBlock(block stateless.Block, status choices.Status)
}
