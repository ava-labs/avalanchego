// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package block

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrHeightIndexedVMNotImplemented = errors.New("vm does not implement HeightIndexedChainVM interface")
	ErrIndexIncomplete               = errors.New("query failed because height index is incomplete")
)

// HeightIndexedChainVM extends ChainVM to allow querying block IDs by height.
type HeightIndexedChainVM interface {
	// IsHeightIndexComplete should return
	// ErrHeightIndexedVMNotImplemented or ErrIndexIncomplete
	// if height index is not supported or is not currently available
	IsHeightIndexComplete() error
	GetBlockIDByHeight(height uint64) (ids.ID, error)
}
