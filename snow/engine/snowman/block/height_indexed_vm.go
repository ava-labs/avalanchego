// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package block

import (
	"errors"

	"github.com/chain4travel/caminogo/ids"
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
	VerifyHeightIndex() error

	// GetBlockIDAtHeight returns the ID of the block that was accepted with
	// [height].
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
}
