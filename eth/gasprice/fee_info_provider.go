// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package gasprice

import (
	"context"
	"math/big"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/rpc"
	lru "github.com/hashicorp/golang-lru"
)

// additional slots in the header cache to allow processing queries
// for previous blocks (with full number of blocks desired) if new
// blocks are added concurrently.
const feeCacheExtraSlots = 5

type feeInfoProvider struct {
	cache   *lru.Cache
	backend OracleBackend
	// [minGasUsed] ensures we don't recommend users pay non-zero tips when other
	// users are paying a tip to unnecessarily expedite block production.
	minGasUsed     uint64
	newHeaderAdded func() // callback used in tests
}

// feeInfo is the type of data stored in feeInfoProvider's cache.
type feeInfo struct {
	baseFee, tip *big.Int // baseFee and min. suggested tip for tx to be included in the block
	timestamp    uint64   // timestamp of the block header
}

// newFeeInfoProvider returns a bounded buffer with [size] slots to
// store [*feeInfo] for the most recently accepted blocks.
func newFeeInfoProvider(backend OracleBackend, minGasUsed uint64, size int) (*feeInfoProvider, error) {
	fc := &feeInfoProvider{
		backend:    backend,
		minGasUsed: minGasUsed,
	}
	if size == 0 {
		// if size is zero, we return early as there is no
		// reason for a goroutine to subscribe to the chain's
		// accepted event.
		fc.cache, _ = lru.New(size)
		return fc, nil
	}

	fc.cache, _ = lru.New(size + feeCacheExtraSlots)
	// subscribe to the chain accepted event
	acceptedEvent := make(chan core.ChainEvent, 1)
	backend.SubscribeChainAcceptedEvent(acceptedEvent)
	go func() {
		for ev := range acceptedEvent {
			fc.addHeader(context.Background(), ev.Block.Header())
			if fc.newHeaderAdded != nil {
				fc.newHeaderAdded()
			}
		}
	}()
	return fc, fc.populateCache(size)
}

// addHeader processes header into a feeInfo struct and caches the result.
func (f *feeInfoProvider) addHeader(ctx context.Context, header *types.Header) (*feeInfo, error) {
	feeInfo := &feeInfo{
		timestamp: header.Time,
		baseFee:   header.BaseFee,
	}

	totalGasUsed := new(big.Int).SetUint64(header.GasUsed)
	minGasUsed := new(big.Int).SetUint64(f.minGasUsed)

	// Don't bias the estimate with blocks containing a limited number of transactions paying to
	// expedite block production.
	var err error
	if minGasUsed.Cmp(totalGasUsed) <= 0 {
		// Compute minimum required tip to be included in previous block
		//
		// NOTE: Using this approach, we will never recommend that the caller
		// provides a non-zero tip unless some block is produced faster than the
		// target rate (which could only occur if some set of callers manually override the
		// suggested tip). In the future, we may wish to start suggesting a non-zero
		// tip when most blocks are full otherwise callers may observe an unexpected
		// delay in transaction inclusion.
		feeInfo.tip, err = f.backend.MinRequiredTip(ctx, header)
	}

	f.cache.Add(header.Number.Uint64(), feeInfo)
	return feeInfo, err
}

// get returns the feeInfo for block with [number] if present in the cache
// and a boolean representing if it was found.
func (f *feeInfoProvider) get(number uint64) (*feeInfo, bool) {
	// Note: use Peek on LRU to use it as a bounded buffer.
	feeInfoIntf, ok := f.cache.Peek(number)
	if ok {
		return feeInfoIntf.(*feeInfo), ok
	}
	return nil, ok
}

// populateCache populates [f] with [size] blocks up to last accepted.
// Note: assumes [size] is greater than zero.
func (f *feeInfoProvider) populateCache(size int) error {
	lastAccepted := f.backend.LastAcceptedBlock().NumberU64()
	lowerBlockNumber := uint64(0)
	if uint64(size-1) <= lastAccepted { // Note: "size-1" because we need a total of size blocks.
		lowerBlockNumber = lastAccepted - uint64(size-1)
	}

	for i := lowerBlockNumber; i <= lastAccepted; i++ {
		header, err := f.backend.HeaderByNumber(context.Background(), rpc.BlockNumber(i))
		if err != nil {
			return err
		}
		_, err = f.addHeader(context.Background(), header)
		if err != nil {
			return err
		}
	}
	return nil
}
