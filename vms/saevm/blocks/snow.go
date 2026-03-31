// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/rlp"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/adaptor"

	// Imported to allow IDE resolution of comments like [types.Block]. The
	// package is imported in other files so this is a no-op beyond devex.
	_ "github.com/ava-labs/libevm/core/types"
)

var _ adaptor.BlockProperties = (*Block)(nil)

// ID returns [types.Block.Hash] from the wrapped [types.Block].
func (b *Block) ID() ids.ID {
	return ids.ID(b.Hash())
}

// Parent returns [types.Block.ParentHash] from the wrapped [types.Block].
func (b *Block) Parent() ids.ID {
	return ids.ID(b.ParentHash())
}

// Bytes returns the RLP encoding of the wrapped [types.Block]. If encoding
// returns an error, it is logged at the ERROR level and a nil slice is
// returned.
func (b *Block) Bytes() []byte {
	buf, err := rlp.EncodeToBytes(b.EthBlock())
	if err != nil {
		b.log.Error("RLP encoding error", zap.Error(err))
		return nil
	}
	return buf
}

// Height returns [types.Block.NumberU64] from the wrapped [types.Block].
func (b *Block) Height() uint64 {
	return b.NumberU64()
}

// Timestamp returns the timestamp of the wrapped [types.Block], at
// [time.Second] resolution.
func (b *Block) Timestamp() time.Time {
	return time.Unix(int64(b.BuildTime()), 0) //nolint:gosec // Won't be a problem for a few millennia
}
