// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/ava-labs/libevm/rpc"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// BatchedParseBlock parses the given blocks concurrently. It returns an error
// if any of the blocks fail to parse.
func (vm *VM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]*blocks.Block, error) {
	return fetchConcurrent(ctx, func(ctx context.Context, i int) (*blocks.Block, error) {
		return vm.ParseBlock(ctx, blks[i])
	}, len(blks))
}

// GetAncestors returns the blocks starting with the block with the given ID and
// continuing with its ancestors, up to the given maximum number of blocks or
// maximum total size of blocks. The returned blocks are in order from the
// requested block to its ancestors. It is assumed that the request block is
// canonical (i.e. accepted). For more details about guarantees, see
// [block.GetAncestors].
//
// The block fetches are done in parallel as performance improvement on disk
// reads.
func (vm *VM) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, maxBlocksSize int, maxBlocksRetrievalTime time.Duration) ([][]byte, error) {
	_ = block.GetAncestors // protect import

	base, err := vm.GetBlock(ctx, blkID)
	switch err {
	case database.ErrNotFound:
		return nil, nil // matches behavior in [block.GetAncestors].
	case nil:
	default:
		return nil, err
	}

	if maxBlocksNum <= 1 {
		return [][]byte{base.Bytes()}, nil
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	parents, err := fetchConcurrent(deadlineCtx, func(_ context.Context, i int) (*blocks.Block, error) {
		idx := uint64(i + 1) //#nosec G115 -- indices are non-negative
		if base.Height() < idx {
			return nil, nil
		}
		height := base.Height() - idx

		b, err := blocks.FromNumberOrHash(
			vm.chain(),
			rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(height)), //#nosec G115 -- won't happen for a while
			func(b *blocks.Block) *blocks.Block {
				return b
			},
			vm.settledBlockFromDB,
		)
		switch {
		case err == database.ErrNotFound:
			return nil, nil
		case errors.Is(err, blocks.ErrFutureBlockNotResolved):
			return nil, nil // canonical block not found, can't fetch by number
		default:
			return b, err
		}
	}, maxBlocksNum-1)
	if err != nil {
		return nil, err
	}

	// Cap at max size
	resp := make([][]byte, 1, len(parents)+1)
	resp[0] = base.Bytes()
	currentByteLength := len(base.Bytes()) + wrappers.IntLen // p2p overhead
	for _, b := range parents {
		if b == nil {
			break
		}
		if deadlineCtx.Err() != nil {
			break
		}

		currentByteLength += len(b.Bytes()) + wrappers.IntLen
		if currentByteLength > maxBlocksSize {
			break
		}
		resp = append(resp, b.Bytes())
	}

	return resp, nil
}

// fetchConcurrent calls the fetch function concurrently the given number of
// times. It may return early if the context is canceled or any error is
// returned. The results are returned in order of the indices passed to the
// fetch function. fetch can return nil and all subsequent entries can be omitted.
func fetchConcurrent[T any](ctx context.Context, fetch func(context.Context, int) (*T, error), num int) ([]*T, error) {
	var mu sync.Mutex
	resp := make([]*T, num)
	firstMissing := math.MaxInt

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for i := range num {
		eg.Go(func() error {
			if egCtx.Err() != nil {
				return nil
			}

			mu.Lock()
			check := firstMissing
			mu.Unlock()
			if i > check {
				return nil // the consumer will clip the response at the first nil
			}

			v, err := fetch(egCtx, i)
			if err != nil {
				return err
			}

			mu.Lock()
			defer mu.Unlock()
			switch {
			case v != nil:
				resp[i] = v
			case i < firstMissing:
				firstMissing = i
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return resp, nil
}
