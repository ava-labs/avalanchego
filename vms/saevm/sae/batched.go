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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
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
func (*VM) BatchedParseBlock(context.Context, [][]byte) ([]*blocks.Block, error) {
	return nil, block.ErrRemoteVMNotImplemented
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
	base, err := vm.GetBlock(ctx, blkID)
	switch {
	case errors.Is(err, database.ErrNotFound):
		return nil, nil // matches behavior in [block.GetAncestors].
	case err != nil:
		return nil, err
	}

	if maxBlocksNum <= 1 {
		return [][]byte{base.Bytes()}, nil
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	var (
		mu              sync.Mutex    // protects all state below
		estimatedLength int           // cumulative size of blocks retrieved so far
		stopAfter       = math.MaxInt // first block index to not find a block
		resp            = make([][]byte, maxBlocksNum)
	)
	resp[0] = base.Bytes()
	estimatedLength = len(resp[0]) + wrappers.IntLen

	eg, egCtx := errgroup.WithContext(deadlineCtx)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for i := 1; i < maxBlocksNum; i++ {
		eg.Go(func() error {
			if egCtx.Err() != nil {
				return nil
			}

			mu.Lock()
			stop := i > stopAfter
			mu.Unlock()
			if stop {
				return nil // a lower index already failed or exceeded the size limit
			}

			idx := uint64(i) //#nosec G115 -- indices are non-negative
			if base.Height() < idx {
				return nil // reached genesis
			}
			height := base.Height() - idx

			b, err := blocks.FromNumberOrHash(
				vm.chain(),
				rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(height)), //#nosec G115 -- won't happen for a while
				func(b *blocks.Block) *blocks.Block {
					return b
				},
				func(db ethdb.Reader, hash common.Hash, num uint64) (*blocks.Block, error) {
					// [VM.settledBlockFromDB] could be used, but the
					// execution results are slow and unneeded.
					ethB := rawdb.ReadBlock(db, hash, num)
					if ethB == nil {
						return nil, database.ErrNotFound
					}
					return blocks.New(ethB, nil, nil, vm.log())
				},
			)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, database.ErrNotFound), errors.Is(err, blocks.ErrFutureBlockNotResolved):
				stopAfter = min(i, stopAfter)
				return nil
			case err != nil:
				return err
			}

			// estimatedLength may include older blocks
			enc := b.Bytes()
			estimatedLength += len(enc) + wrappers.IntLen
			if estimatedLength > maxBlocksSize {
				stopAfter = min(i, stopAfter)
			}

			resp[i] = enc
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Cap at max size, include at least 1 block
	currentByteLength := len(resp[0]) + wrappers.IntLen
	i := 1
	for ; i < len(resp); i++ {
		b := resp[i]
		if b == nil {
			break
		}

		currentByteLength += len(b) + wrappers.IntLen
		if currentByteLength > maxBlocksSize {
			break
		}
	}

	return resp[:i], nil
}
