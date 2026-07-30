// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
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
// requested block to its ancestors. Only accepted blocks are served, any other
// block is treated as not found. For more details about guarantees, see
// [block.GetAncestors].
//
// The requested block only has its height resolved individually. All block
// contents, its own included, are then streamed by a single pass of two
// database iterators, one over headers and canonical hashes and one over
// bodies, as ancestors occupy contiguous heights. The pass walks backward
// from the requested height, reading exactly the blocks in the response.
// If the database cannot iterate backward the pass instead scans forward
// across the full requested range, reading blocks below a maxBlocksSize cut
// wastefully, and the response MAY be empty if maxBlocksRetrievalTime expires
// mid-scan.
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

	if resp, ok, err := ancestorsDescending(vm.db, hash, lo, baseHeight, maxBlocksSize); ok || err != nil {
		return resp, err
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	stored, err := readCanonicalRLPRange(deadlineCtx, vm.db, lo, baseHeight)
	if err != nil {
		return nil, err
	}
	if top := stored[len(stored)-1]; top.hash != hash || top.header == nil || top.body == nil {
		return nil, nil // matches behavior in [block.GetAncestors].
	}
	return ancestorsResponse(stored, maxBlocksSize)
}

// A prevIterator can iterate backward; see [database.Iterator.Prev].
type prevIterator interface {
	ethdb.Iterator
	Prev() bool
}

// A descendingCursor exposes the current entry of a [prevIterator] while
// walking backward. Keys and values are retained under the same iterator
// stability requirement documented on [readCanonicalRLPRange].
type descendingCursor struct {
	it       prevIterator
	key, val []byte
	ok       bool
}

func (c *descendingCursor) prev() {
	c.ok = c.it.Prev()
	if c.ok {
		c.key = c.it.Key()
		c.val = c.it.Value()
	} else {
		c.key = nil
		c.val = nil
	}
}

// scannedHeader is one height's canonical hash and header, streamed from the
// header scan to the assembler of [ancestorsDescending].
type scannedHeader struct {
	height uint64
	hash   common.Hash  // canonical mapping at height, zero if absent
	header rlp.RawValue // the canonical block's header, nil if not stored
}

// scannedBody is one height's stored body, streamed from the body scan to the
// assembler of [ancestorsDescending].
type scannedBody struct {
	height uint64
	hash   common.Hash // the hash of the block the body belongs to
	body   rlp.RawValue
}

// scanBatchSize is the number of heights per message on the scan channels of
// [ancestorsDescending], amortising channel synchronisation across many
// heights. Batches are fixed arrays passed by value, so no allocation ever
// escapes to the heap for them beyond the channels' own buffers.
const scanBatchSize = 64

// scanChannelDepth is the buffer size of the scan channels of
// [ancestorsDescending], decoupling bursts in scan and assembly speed.
const scanChannelDepth = 1

// A scanBatch carries up to [scanBatchSize] records between the scan
// goroutines and the assembler of [ancestorsDescending].
type scanBatch[T any] struct {
	n    int
	recs [scanBatchSize]T
}

// A batchSender accumulates records and sends them in [scanBatch] units.
type batchSender[T any] struct {
	ctx   context.Context
	out   chan<- scanBatch[T]
	batch scanBatch[T]
}

// push adds a record, flushing a full batch. It reports false once ctx is
// done.
func (s *batchSender[T]) push(rec T) bool {
	s.batch.recs[s.batch.n] = rec
	s.batch.n++
	if s.batch.n < scanBatchSize {
		return true
	}
	return s.flush()
}

// flush sends any accumulated records. It reports false once ctx is done.
func (s *batchSender[T]) flush() bool {
	if s.batch.n == 0 {
		return true
	}
	select {
	case s.out <- s.batch:
		s.batch.n = 0
		return true
	case <-s.ctx.Done():
		return false
	}
}

// A batchReceiver yields the records of a channel of batches one at a time.
type batchReceiver[T any] struct {
	ch    <-chan scanBatch[T]
	batch scanBatch[T]
	i     int
}

func (r *batchReceiver[T]) next() (T, bool) {
	for r.i == r.batch.n {
		batch, ok := <-r.ch
		if !ok {
			var zero T
			return zero, false
		}
		r.batch = batch
		r.i = 0
	}
	rec := r.batch.recs[r.i]
	r.i++
	return rec, true
}

// ancestorsDescending serves [VM.GetAncestors] by walking two backward
// iterators from baseHeight towards lo, stopping as soon as the response is
// complete so that only returned blocks are read. The iterators walk in their
// own goroutines, streaming per-height records to the assembler, so their
// reads overlap each other and the response encoding. It reports false,
// without consuming any data, if the database does not support backward
// iteration.
func ancestorsDescending(db ethdb.Database, hash common.Hash, lo, baseHeight uint64, maxBlocksSize int) (_ [][]byte, supported bool, _ error) {
	// The iterators cover their whole keyspaces, positioned so that the first
	// backward step lands on the highest key of baseHeight.
	limit := binary.BigEndian.AppendUint64(nil, baseHeight+1)
	rawHeaders := db.NewIterator(headerPrefix, limit)
	defer rawHeaders.Release()
	rawBodies := db.NewIterator(blockBodyPrefix, limit)
	defer rawBodies.Release()

	headers, ok := rawHeaders.(prevIterator)
	if !ok {
		return nil, false, nil
	}
	bodies, ok := rawBodies.(prevIterator)
	if !ok {
		return nil, false, nil
	}

	hCur := &descendingCursor{it: headers}
	bCur := &descendingCursor{it: bodies}
	hCur.prev()
	bCur.prev()
	if !hCur.ok && errors.Is(headers.Error(), database.ErrPrevNotSupported) {
		return nil, false, nil
	}
	if !bCur.ok && errors.Is(bodies.Error(), database.ErrPrevNotSupported) {
		return nil, false, nil
	}

	// The scans exit once their keyspace is exhausted or, via the deferred
	// cancel, once the response is complete.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		headerCh = make(chan scanBatch[scannedHeader], scanChannelDepth)
		bodyCh   = make(chan scanBatch[scannedBody], scanChannelDepth)
		eg       errgroup.Group
	)
	eg.Go(func() error {
		defer close(headerCh)
		return scanHeadersDescending(ctx, db, hCur, headerCh)
	})
	eg.Go(func() error {
		defer close(bodyCh)
		return scanBodiesDescending(ctx, bCur, bodyCh)
	})

	resp, err := assembleDescending(db, hash, lo, baseHeight, maxBlocksSize, headerCh, bodyCh)
	cancel()
	if egErr := eg.Wait(); err == nil {
		err = egErr
	}
	if err != nil {
		return nil, true, err
	}
	return resp, true, nil
}

// scanHeadersDescending walks the header keyspace backward from cur's
// position, streaming each height's canonical hash and header. Heights
// without any stored keys are skipped rather than streamed.
func scanHeadersDescending(ctx context.Context, db ethdb.Database, cur *descendingCursor, out chan<- scanBatch[scannedHeader]) error {
	sender := batchSender[scannedHeader]{ctx: ctx, out: out}
	for cur.ok {
		payloadLen := len(cur.key) - len(headerPrefix) - rawdbNumLen
		if payloadLen < 0 {
			cur.prev()
			continue
		}
		rec := scannedHeader{
			height: binary.BigEndian.Uint64(cur.key[len(headerPrefix):]),
		}
		var candidateHash common.Hash
		for cur.ok {
			key := cur.key
			payloadLen := len(key) - len(headerPrefix) - rawdbNumLen
			if payloadLen >= 0 && binary.BigEndian.Uint64(key[len(headerPrefix):]) != rec.height {
				break // walking backward, so a different height is below
			}
			switch payloadLen {
			case len(headerHashSuffix):
				if bytes.HasSuffix(key, headerHashSuffix) {
					rec.hash = common.BytesToHash(cur.val)
				}
			case common.HashLength:
				candidateHash = common.BytesToHash(key[len(key)-common.HashLength:])
				rec.header = cur.val
			}
			cur.prev()
		}
		switch {
		case rec.hash == (common.Hash{}):
			// Without a canonical mapping, any stored artefacts are dangling
			// leftovers rather than canonical blocks.
			rec.header = nil
		case rec.header != nil && candidateHash != rec.hash:
			// See the sibling fallback rationale in [readCanonicalRLPRange].
			rec.header = rawdb.ReadHeaderRLP(db, rec.hash, rec.height)
		}

		if !sender.push(rec) {
			return nil
		}
	}
	if !sender.flush() {
		return nil
	}
	return cur.it.Error()
}

// scanBodiesDescending walks the body keyspace backward from cur's position,
// streaming each height's stored body. Heights without any stored keys are
// skipped rather than streamed.
func scanBodiesDescending(ctx context.Context, cur *descendingCursor, out chan<- scanBatch[scannedBody]) error {
	sender := batchSender[scannedBody]{ctx: ctx, out: out}
	for cur.ok {
		payloadLen := len(cur.key) - len(blockBodyPrefix) - rawdbNumLen
		if payloadLen < 0 {
			cur.prev()
			continue
		}
		rec := scannedBody{
			height: binary.BigEndian.Uint64(cur.key[len(blockBodyPrefix):]),
		}
		for cur.ok {
			key := cur.key
			payloadLen := len(key) - len(blockBodyPrefix) - rawdbNumLen
			if payloadLen >= 0 && binary.BigEndian.Uint64(key[len(blockBodyPrefix):]) != rec.height {
				break
			}
			if payloadLen == common.HashLength {
				rec.hash = common.BytesToHash(key[len(key)-common.HashLength:])
				rec.body = cur.val
			}
			cur.prev()
		}

		if !sender.push(rec) {
			return nil
		}
	}
	if !sender.flush() {
		return nil
	}
	return cur.it.Error()
}

// assembleDescending zips the two scan streams by height, splices each block
// and appends it to the response, applying the rules documented on
// [VM.GetAncestors]. A nil response means the base block is not stored as
// canonical.
func assembleDescending(db ethdb.Database, hash common.Hash, lo, baseHeight uint64, maxBlocksSize int, headerCh <-chan scanBatch[scannedHeader], bodyCh <-chan scanBatch[scannedBody]) ([][]byte, error) {
	var (
		headers = batchReceiver[scannedHeader]{ch: headerCh}
		bodies  = batchReceiver[scannedBody]{ch: bodyCh}
	)
	hRec, hOK := headers.next()
	bRec, bOK := bodies.next()

	var (
		resp     = make([][]byte, 0, baseHeight-lo+1)
		sizeLeft = maxBlocksSize
	)
	for h := baseHeight; ; h-- {
		// Streams descend and skip empty heights, so a stream whose record is
		// below h has nothing stored at h.
		var s storedBlockRLP
		if hOK && hRec.height == h {
			s.hash = hRec.hash
			s.header = hRec.header
			hRec, hOK = headers.next()
		}
		if bOK && bRec.height == h {
			if s.hash != (common.Hash{}) && bRec.hash != s.hash {
				// See the sibling fallback rationale in
				// [readCanonicalRLPRange].
				s.body = rawdb.ReadBodyRLP(db, s.hash, h)
			} else {
				s.body = bRec.body
			}
			bRec, bOK = bodies.next()
		}

		missing := s.hash == (common.Hash{}) || s.header == nil || s.body == nil
		if h == baseHeight && (s.hash != hash || missing) {
			return nil, nil // matches behavior in [block.GetAncestors].
		}
		if missing {
			// Accepted blocks are written to disk before [VM.AcceptBlock]
			// returns, so a missing block means the remaining ancestry is not
			// canonical.
			return resp, nil
		}
		enc, err := spliceStored(&s)
		if err != nil {
			return nil, err
		}
		size := len(enc) + wrappers.IntLen
		// The requested block is always returned, even if it alone exceeds
		// maxBlocksSize.
		if len(resp) > 0 && size > sizeLeft {
			return resp, nil
		}
		sizeLeft -= size
		resp = append(resp, enc)
		if h == lo {
			return resp, nil
		}
	}
}

// ancestorsResponse converts a scanned height range into the response of
// [VM.GetAncestors], the consensus encodings as defined by
// [blocks.Block.Bytes], descending from the range's highest block. The highest
// block is always included, even if it alone exceeds maxBlocksSize. Further
// blocks are appended until one would exceed maxBlocksSize, each costing its
// length plus [wrappers.IntLen], or is missing.
func ancestorsResponse(stored []storedBlockRLP, maxBlocksSize int) ([][]byte, error) {
	var (
		resp     = make([][]byte, 0, len(stored))
		sizeLeft = maxBlocksSize
	)
	for i := len(stored) - 1; i >= 0; i-- {
		s := &stored[i]
		if s.header == nil || s.body == nil {
			// Accepted blocks are written to disk before [VM.AcceptBlock]
			// returns, so a missing block means the remaining ancestry is not
			// canonical, e.g. below a deadline-abandoned scan.
			break
		}
		enc, err := spliceStored(s)
		if err != nil {
			return nil, err
		}
		size := len(enc) + wrappers.IntLen
		if len(resp) > 0 && size > sizeLeft {
			break
		}
		sizeLeft -= size
		resp = append(resp, enc)
	}
	return resp, nil
}

// spliceStored converts a single stored block into its consensus encoding.
func spliceStored(s *storedBlockRLP) ([]byte, error) {
	enc, err := blocks.SpliceBlockRLP(s.header, s.body)
	if err != nil {
		return nil, fmt.Errorf("splicing stored block %#x: %v", s.hash, err)
	}
	return enc, nil
}

// [rawdb] does not export iterator-based readers for headers and bodies so the
// relevant fragments of its key schema are replicated here, verbatim.
// TestReadCanonicalRLPRange guards against upstream drift.
const rawdbNumLen = 8 // big-endian uint64 block number

var (
	headerPrefix     = []byte("h") // headerPrefix + num + hash -> header
	headerHashSuffix = []byte("n") // headerPrefix + num + headerHashSuffix -> canonical hash
	blockBodyPrefix  = []byte("b") // blockBodyPrefix + num + hash -> block body
)

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
// [newEthDB] qualifies, as do the in-memory databases used in tests.
func readCanonicalRLPRange(ctx context.Context, db ethdb.Database, from, to uint64) ([]storedBlockRLP, error) {
	stored := make([]storedBlockRLP, to-from+1)

	scan := func(prefix []byte, fn func(offset uint64, key, value []byte)) error {
		it := db.NewIterator(prefix, binary.BigEndian.AppendUint64(nil, from))
		defer it.Release()
		// Checking the deadline on every key would cost more than it saves,
		// so it is polled every deadlineCheckMask+1 keys.
		const deadlineCheckMask = 1<<10 - 1
		for n := 0; it.Next(); n++ {
			if n&deadlineCheckMask == deadlineCheckMask && ctx.Err() != nil {
				break
			}
			key := it.Key()
			if len(key) < len(prefix)+rawdbNumLen {
				continue
			}
			num := binary.BigEndian.Uint64(key[len(prefix):])
			if num > to {
				break
			}
			fn(num-from, key, it.Value())
		}
		return it.Error()
	}

	// A height's canonical-hash key sorts among its header keys, depending on
	// the first byte of each hash, so headers cannot be filtered mid-scan.
	// They are instead verified against the canonical hash below.
	//
	// The two scans touch disjoint fields of `stored` so they run concurrently
	// to overlap their disk reads.
	headerHashes := make([]common.Hash, len(stored))
	bodyHashes := make([]common.Hash, len(stored))
	var eg errgroup.Group
	eg.Go(func() error {
		return scan(headerPrefix, func(i uint64, key, value []byte) {
			switch len(key) - len(headerPrefix) - rawdbNumLen {
			case len(headerHashSuffix):
				if bytes.HasSuffix(key, headerHashSuffix) {
					stored[i].hash = common.BytesToHash(value)
				}
			case common.HashLength:
				headerHashes[i] = common.BytesToHash(key[len(key)-common.HashLength:])
				stored[i].header = value
			}
		})
	})
	eg.Go(func() error {
		return scan(blockBodyPrefix, func(i uint64, key, value []byte) {
			if len(key)-len(blockBodyPrefix)-rawdbNumLen != common.HashLength {
				return
			}
			bodyHashes[i] = common.BytesToHash(key[len(key)-common.HashLength:])
			stored[i].body = value
		})
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
