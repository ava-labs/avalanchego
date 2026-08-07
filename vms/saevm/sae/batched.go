// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"golang.org/x/sync/errgroup"

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
// requested block to its ancestors. Only accepted blocks are served, any other
// block is treated as not found. For more details about guarantees, see
// [block.GetAncestors].
//
// The requested block only has its height resolved individually. All block
// contents, its own included, are then read by database iterators over
// headers, canonical hashes and bodies, as ancestors occupy contiguous
// heights. The iterators only advance from lower heights to higher, so the
// range is read in pages of consecutive heights, newest page first, stopping
// within a page of the maxBlocksSize cut. If maxBlocksRetrievalTime expires
// mid-read the response is truncated, and MAY be empty if it expires while
// the requested block's own page is read.
func (vm *VM) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, maxBlocksSize int, maxBlocksRetrievalTime time.Duration) ([][]byte, error) {
	// Only accepted blocks have a hash-to-number mapping.
	hash := common.Hash(blkID)
	num := rawdb.ReadHeaderNumber(vm.db, hash)
	if num == nil {
		return nil, nil // matches behavior in [block.GetAncestors].
	}
	baseHeight := *num

	var (
		numBlocks = min(uint64(max(maxBlocksNum, 1)), baseHeight+1) //#nosec G115 -- non-negative by max()
		lo        = baseHeight + 1 - numBlocks                      // lowest height to return
	)

	deadlineCtx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	return ancestorsDescending(deadlineCtx, vm.db, hash, lo, baseHeight, maxBlocksSize)
}

// ancestorsPageSize is the number of heights per page of
// [ancestorsDescending]. Reads past the response's end are bounded by a page,
// so smaller pages waste less, while each page pays iterator setup and a
// re-seek, so larger pages amortise better. 128 measured fastest.
const ancestorsPageSize = 128

// A readPage carries one page of scanned heights, or the scan's error, from
// the page reader to the assembler of [ancestorsDescending].
type readPage struct {
	stored []storedBlockRLP
	err    error
}

// ancestorsDescending serves [VM.GetAncestors] by reading pages of up to
// [ancestorsPageSize] consecutive heights, newest page first, splicing each
// page's blocks onto the response, newest first. The page below is read while
// the current one is assembled, so the reads overlap the splicing, and once
// the response is complete no further pages are read. A nil response means
// the block at baseHeight is not stored as canonical with the given hash.
//
// The requested block is always returned, even if it alone exceeds
// maxBlocksSize. Further blocks are appended until one would exceed
// maxBlocksSize, each costing its length plus [wrappers.IntLen], or is
// missing.
func ancestorsDescending(ctx context.Context, db ethdb.Database, hash common.Hash, lo, baseHeight uint64, maxBlocksSize int) ([][]byte, error) {
	// The page reader exits once it has sent the lo page or, via the deferred
	// cancel, once the response is complete.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages := make(chan readPage)
	go func() {
		defer close(pages)
		for hi := baseHeight; ; {
			pageLo := lo
			if hi-lo >= ancestorsPageSize {
				pageLo = hi - ancestorsPageSize + 1
			}
			stored, err := readCanonicalRLPRange(ctx, db, pageLo, hi)
			select {
			case pages <- readPage{stored: stored, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil || pageLo == lo || ctx.Err() != nil {
				return
			}
			hi = pageLo - 1
		}
	}()

	var (
		resp     = make([][]byte, 0, baseHeight-lo+1)
		sizeLeft = maxBlocksSize
	)
	for page := range pages {
		if page.err != nil {
			return nil, page.err
		}
		for i := len(page.stored) - 1; i >= 0; i-- {
			s := &page.stored[i]
			missing := s.hash == (common.Hash{}) || s.header == nil || s.body == nil
			if len(resp) == 0 && (s.hash != hash || missing) {
				return nil, nil // matches behavior in [block.GetAncestors].
			}
			if missing {
				// Accepted blocks are written to disk before [VM.AcceptBlock]
				// returns, so a missing block means the remaining ancestry is
				// not canonical, e.g. beyond an expired deadline.
				return resp, nil
			}
			enc, err := types.BlockBytes(s.header, s.body)
			if err != nil {
				return nil, fmt.Errorf("splicing stored block %#x: %v", s.hash, err)
			}
			size := len(enc) + wrappers.IntLen
			if len(resp) > 0 && size > sizeLeft {
				return resp, nil
			}
			sizeLeft -= size
			resp = append(resp, enc)
		}
	}
	return resp, nil
}

// A storedBlockRLP holds the database encodings of a canonical block.
type storedBlockRLP struct {
	hash         common.Hash  // canonical hash, zero if none is stored
	header, body rlp.RawValue // nil if not stored
}

// readCanonicalRLPRange reads the canonical hashes and stored block encodings
// of heights [from, to], indexed by offset from `from`. Heights are contiguous
// in the key schema so all blocks are read with two sequential scans, one over
// headers and canonical hashes, which share a key space, and one over bodies.
// Sequential scans are substantially faster than the three random point reads
// per block that [rawdb.ReadBlock] would incur.
//
// If ctx expires mid-scan the remaining heights are left unread, which the
// caller cannot distinguish from missing blocks.
//
// Iterator values are retained without copying, so the database's iterators
// MUST return slices that remain valid after Next and Release. The
// [ethdb.Iterator] contract alone does not promise this, but every
// [database.Iterator] implementation does, so any database wrapped by
// [github.com/ava-labs/avalanchego/vms/saevm/types.NewEthDB] qualifies, as do
// the in-memory databases used in tests.
func readCanonicalRLPRange(ctx context.Context, db ethdb.Database, from, to uint64) ([]storedBlockRLP, error) {
	stored := make([]storedBlockRLP, to-from+1)

	// Checking the deadline on every entry would cost more than it saves, so
	// it is polled every deadlineCheckMask+1 entries.
	const deadlineCheckMask = 1<<10 - 1

	// A height's canonical-hash entry sorts among its header entries,
	// depending on the first byte of each hash, so headers cannot be filtered
	// mid-scan. They are instead verified against the canonical hash below.
	//
	// The two scans touch disjoint fields of `stored` so they run concurrently
	// to overlap their disk reads.
	headerHashes := make([]common.Hash, len(stored))
	bodyHashes := make([]common.Hash, len(stored))
	var eg errgroup.Group
	eg.Go(func() error {
		n := 0
		for h, err := range rawdb.Headers(db, from) {
			if err != nil {
				return err
			}
			if h.Number > to || n&deadlineCheckMask == deadlineCheckMask && ctx.Err() != nil {
				break
			}
			n++
			i := h.Number - from
			if h.RLP == nil {
				stored[i].hash = h.Hash
			} else {
				headerHashes[i] = h.Hash
				stored[i].header = h.RLP
			}
		}
		return nil
	})
	eg.Go(func() error {
		n := 0
		for b, err := range rawdb.Bodies(db, from) {
			if err != nil {
				return err
			}
			if b.Number > to || n&deadlineCheckMask == deadlineCheckMask && ctx.Err() != nil {
				break
			}
			n++
			i := b.Number - from
			bodyHashes[i] = b.Hash
			stored[i].body = b.RLP
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	for i := range stored {
		s := &stored[i]
		if s.hash == (common.Hash{}) {
			// Without a canonical mapping, any stored artefacts are dangling
			// leftovers rather than canonical blocks.
			s.header = nil
			s.body = nil
			continue
		}
		// Current versions only write canonical blocks but older versions
		// also wrote non-canonical ones, so a height may hold several headers
		// and bodies. The scan kept the last, possibly non-canonical, one.
		// Fall back to point reads of the canonical block.
		num := from + uint64(i) //#nosec G115 -- bounded by to
		if s.header != nil && headerHashes[i] != s.hash {
			s.header = rawdb.ReadHeaderRLP(db, s.hash, num)
		}
		if s.body != nil && bodyHashes[i] != s.hash {
			s.body = rawdb.ReadBodyRLP(db, s.hash, num)
		}
	}
	return stored, nil
}
