// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package block

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrHeightIndexedVMNotImplemented = errors.New("vm does not implement HeightIndexedChainVM interface")

// HeightIndexedChainVM extends the minimal functionalities exposed by ChainVM to allow querying
// block IDs by height.
type HeightIndexedChainVM interface {
	GetBlockIDByHeight(height uint64) (ids.ID, error)
}
