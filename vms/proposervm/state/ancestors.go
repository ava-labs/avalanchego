// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// DefaultAncestorsConcurrency is the number of block reads issued at once when
// serving GetAncestors.
//
// Walking ancestors is bound by read latency rather than by CPU: the blocks are
// keyed by ID, so each one is an independent random read. Chasing parent
// pointers forces those reads to happen one at a time, because a block's parent
// is only known once the block has been read. Resolving the range from the
// height index removes that dependency and lets the reads overlap.
const DefaultAncestorsConcurrency = 16

// GetAncestorBytes returns the serialized blocks at heights [topHeight] down to
// [topHeight]-[maxBlocksNum]+1, newest first, subject to the same size and time
// limits as the serial walk.
//
// Blocks are fetched in waves of [concurrency], with the limits checked between
// waves so that no more is read than a response can carry. Fewer blocks than
// requested are returned when the range runs past what the height index holds,
// which is how the caller learns it has reached the fork height.
//
// The heights are assumed to be accepted and contiguous; the caller is
// responsible for having checked that [topHeight] holds the block it was asked
// for, since the height index describes only the accepted chain.
func GetAncestorBytes(
	bs BlockState,
	hi HeightIndexGetter,
	topHeight uint64,
	maxBlocksNum int,
	maxBlocksSize int,
	deadline time.Time,
	now func() time.Time,
	concurrency int,
) ([][]byte, error) {
	if maxBlocksNum <= 0 {
		return nil, nil
	}
	if concurrency <= 0 {
		concurrency = DefaultAncestorsConcurrency
	}

	// The walk cannot go below the fork height: earlier blocks were not wrapped
	// by this VM and are not in its block store.
	lowest := uint64(0)
	if forkHeight, err := hi.GetForkHeight(); err == nil {
		lowest = forkHeight
	}

	var (
		res       = make([][]byte, 0, maxBlocksNum)
		byteLen   = 0
		height    = topHeight
		exhausted = false
	)
	for len(res) < maxBlocksNum && height >= lowest && !exhausted {
		wave := min(concurrency, maxBlocksNum-len(res))
		if remaining := height - lowest + 1; uint64(wave) > remaining {
			wave = int(remaining)
		}

		// Ascending by height, so reverse below to walk newest first.
		blkIDs, err := hi.GetBlockIDsInRange(height+1-uint64(wave), height)
		if err != nil {
			return res, err
		}
		if len(blkIDs) < wave {
			// The index ran out before the requested range; take what is there
			// and stop after this wave.
			exhausted = true
			if len(blkIDs) == 0 {
				break
			}
		}
		for i, j := 0, len(blkIDs)-1; i < j; i, j = i+1, j-1 {
			blkIDs[i], blkIDs[j] = blkIDs[j], blkIDs[i]
		}

		blks, err := fetchBlockBytes(bs, blkIDs, concurrency)
		if err != nil {
			return res, err
		}

		for _, blkBytes := range blks {
			// Include wrappers.IntLen because the size of each container is
			// sent alongside it.
			byteLen += wrappers.IntLen + len(blkBytes)
			if len(res) > 0 && byteLen > maxBlocksSize {
				return res, nil
			}
			res = append(res, blkBytes)
		}

		if len(res) >= maxBlocksNum || !now().Before(deadline) {
			return res, nil
		}
		if height < uint64(len(blkIDs)) {
			break
		}
		height -= uint64(len(blkIDs))
	}
	return res, nil
}

// fetchBlockBytes reads [blkIDs] using at most [concurrency] goroutines,
// preserving order. A block that cannot be read truncates the result rather
// than failing the whole response, matching the serial walk's behaviour of
// returning what it has.
func fetchBlockBytes(bs BlockState, blkIDs []ids.ID, concurrency int) ([][]byte, error) {
	if len(blkIDs) == 1 {
		blkBytes, _, err := bs.GetBlockBytesAndParent(blkIDs[0])
		if err != nil {
			return nil, nil //nolint:nilerr // a missing block truncates the response
		}
		return [][]byte{blkBytes}, nil
	}

	var (
		out  = make([][]byte, len(blkIDs))
		errs = make([]error, len(blkIDs))
		work = make(chan int, len(blkIDs))
		wg   sync.WaitGroup
	)
	for i := range blkIDs {
		work <- i
	}
	close(work)

	workers := min(concurrency, len(blkIDs))
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range work {
				out[i], _, errs[i] = bs.GetBlockBytesAndParent(blkIDs[i])
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return out[:i], nil
		}
	}
	return out, nil
}
