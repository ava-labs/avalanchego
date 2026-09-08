// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
)

// GetAncestors returns the accepted block at height num followed by its
// ancestors, in descending height order.
//
// The walk stops once any of the following conditions are met:
//   - maxBlocks blocks have been served
//   - the next block would push the total size past maxSize
//   - ctx is done
//
// The first block is exempt from all three limits, so it is served whenever it
// exists.
//
// A height with no accepted block yields no blocks and no error.
func GetAncestors(
	ctx context.Context,
	db ethdb.Reader,
	num uint64,
	maxBlocks int,
	maxSize int,
) ([][]byte, error) {
	hash := rawdb.ReadCanonicalHash(db, num)
	if hash == (common.Hash{}) {
		return nil, nil // no accepted block at this height
	}

	// TODO(StephenButtolph): Measure the performance impact of iterative
	// fetching rather than using DB iterators on real databases.
	var (
		numBlocks = min(
			uint64(max(maxBlocks, 1)), //#nosec G115 -- non-negative by max()
			num+1,
		)
		blocks = make([][]byte, 0, numBlocks)
		size   int
	)
	for range numBlocks {
		// An empty result tells the caller the requested block is missing, so
		// serve it even past the deadline.
		if len(blocks) > 0 && ctx.Err() != nil {
			break
		}

		header := rawdb.ReadHeaderRLP(db, hash, num)
		if header == nil {
			break
		}
		body := rawdb.ReadBodyRLP(db, hash, num)
		if body == nil {
			break
		}
		block, err := types.BlockBytes(header, body)
		if err != nil {
			return nil, fmt.Errorf("splicing stored block %d: %v", num, err)
		}

		// The first block is exempt from maxSize, so very large blocks remain
		// servable.
		size += len(block)
		if len(blocks) > 0 && size > maxSize {
			break
		}
		blocks = append(blocks, block)

		// Although the last iteration may underflow num, the loop will exit
		// before reading num again.
		num--
		hash = types.HeaderParentHashFromRLP(header)
	}
	return blocks, nil //nolint:nilerr // a done ctx truncates the walk rather than failing it
}
