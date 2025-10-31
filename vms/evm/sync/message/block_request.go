// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Request = (*BlockRequest)(nil)

// BlockRequest is a request to retrieve the specified number of parent blocks
// starting from the given hash, ordered from newest to oldest.
type BlockRequest struct {
	Hash    common.Hash `serialize:"true"`
	Height  uint64      `serialize:"true"`
	Parents uint16      `serialize:"true"`
}

func (b BlockRequest) String() string {
	return fmt.Sprintf(
		"BlockRequest(Hash=%s, Height=%d, Parents=%d)",
		b.Hash, b.Height, b.Parents,
	)
}

func (b BlockRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleBlockRequest(ctx, nodeID, requestID, b)
}

// BlockResponse is a response to a BlockRequest.
// Blocks is a slice of RLP-encoded blocks starting with the block
// requested in BlockRequest.Hash. The next block is the parent, etc.
type BlockResponse struct {
	Blocks [][]byte `serialize:"true"`
}
