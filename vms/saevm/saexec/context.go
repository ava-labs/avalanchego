// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

var _ core.ChainContext = (*chainContext)(nil)

type chainContext struct {
	headers saetypes.HeaderSource
	recent  *lru.Cache[uint64, *types.Header]
	log     logging.Logger
}

func (c *chainContext) GetHeader(h common.Hash, n uint64) *types.Header {
	if hdr, ok := c.recent.Get(n); ok && hdr.Hash() == h {
		return types.CopyHeader(hdr)
	}
	// eth_call on historical state will miss the cache but we still need to
	// support BLOCKHASH.
	hdr, ok := c.headers(h, n)
	if !ok {
		return nil
	}
	// We explicitly DO NOT populate the cache with these historical values
	// because they'll evict the recent headers, which are populated by
	// [Executor.execute] for use by BLOCKHASH in newly executed blocks.
	return hdr
}

func (c *chainContext) Engine() consensus.Engine {
	// This is serious enough that it needs to be investigated immediately, but
	// not enough to be fatal. It will also cause tests to fail if ever called,
	// so we can catch it early.
	c.log.Error("ChainContext.Engine() called unexpectedly")
	return nil
}
