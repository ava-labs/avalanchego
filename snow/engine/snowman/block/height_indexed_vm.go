// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package block

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrHeightIndexedVMNotImplemented = errors.New("vm does not implement HeightIndexedChainVM interface")
	ErrIndexIncomplete               = errors.New("query failed because height index is incomplete")
)

// HeightIndexedChainVM extends ChainVM to allow querying block IDs by height.
type HeightIndexedChainVM interface {
	// VerifyHeightIndex should return:
	// - nil if the height index is available.
	// - ErrHeightIndexedVMNotImplemented if the height index is not supported.
	// - ErrIndexIncomplete if the height index is not currently available.
	// - Any other non-standard error that may have occurred when verifying the
	//   index.
	VerifyHeightIndex(context.Context) error

	// GetBlockIDAtHeight returns:
	// - The ID of the block that was accepted with [height].
	// - database.ErrNotFound if the [height] index is unknown.
	//
	// Note: A returned value of [database.ErrNotFound] typically means that the
	//       underlying VM was state synced and does not have access to the
	//       blockID at [height].
	GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error)
}
