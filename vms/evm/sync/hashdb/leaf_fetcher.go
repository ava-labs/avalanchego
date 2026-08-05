// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const defaultLeafWorkers = 8

var (
	errEmptyLeafResponse = errors.New("empty leaf response must include a proof")
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errInvalidRangeProof = errors.New("invalid range proof")
	errMoreWithoutKeys   = errors.New("more leaves reported but none returned")
)

// LeafBatch is a verified run of leaves in ascending key order. keys and vals are
// index-aligned, guaranteed by the range proof that produced them.
type LeafBatch struct {
	keys [][]byte
	vals [][]byte
}

// Keys returns the batch's keys in ascending order, index-aligned with [LeafBatch.Vals].
func (b LeafBatch) Keys() [][]byte { return b.keys }

// Vals returns the batch's values, index-aligned with [LeafBatch.Keys].
func (b LeafBatch) Vals() [][]byte { return b.vals }

// lastKey returns the highest key, the next request's start. Not valid when empty.
func (b LeafBatch) lastKey() []byte {
	return b.keys[len(b.keys)-1]
}

// truncate drops leaves past end and reports whether it cut any, meaning the range is
// exhausted. An empty end is a no-op.
func (b *LeafBatch) truncate(end []byte) bool {
	if len(end) == 0 {
		return false
	}
	// Keys ascend, so the first one past end bounds the batch.
	n := sort.Search(len(b.keys), func(i int) bool { return !withinRange(b.keys[i], end) })
	if n == len(b.keys) {
		return false
	}
	b.keys, b.vals = b.keys[:n], b.vals[:n]
	return true
}

// Task is one unit of leaf work the fetcher drives: a contiguous key range of a trie,
// with callbacks per batch and on completion. Implemented by [stateSegment].
type Task interface {
	Root() common.Hash
	Account() common.Hash
	Start() []byte
	// End is the inclusive last key of the range, or nil for the whole trie.
	End() []byte
	OnLeaves(ctx context.Context, batch LeafBatch) error
	OnFinish(ctx context.Context) error
}

// LeafFetcher pulls tasks off a channel and fetches each one's leaves with a pool of
// workers, handing every batch to the Task, which is what reconstructs. Batches are
// verified in the fetch path, not in the transport.
type LeafFetcher struct {
	log        logging.Logger
	client     *Client
	tasks      <-chan Task
	numWorkers int
}

func NewLeafFetcher(log logging.Logger, client *Client, tasks <-chan Task, numWorkers int) *LeafFetcher {
	if numWorkers <= 0 {
		numWorkers = defaultLeafWorkers
	}
	return &LeafFetcher{log: log, client: client, tasks: tasks, numWorkers: numWorkers}
}

// Sync runs the workers until tasks is drained and closed, or ctx ends.
func (f *LeafFetcher) Sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for range f.numWorkers {
		eg.Go(func() error { return f.workerLoop(egCtx) })
	}
	return eg.Wait()
}

// workerLoop processes tasks until the channel closes or ctx ends.
func (f *LeafFetcher) workerLoop(ctx context.Context) error {
	for {
		select {
		case t, ok := <-f.tasks:
			if !ok {
				return nil
			}
			if err := f.syncTask(ctx, t); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncTask walks the Task's range left to right until it is exhausted or End is reached.
func (f *LeafFetcher) syncTask(ctx context.Context, t Task) error {
	start := t.Start()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		batch, more, err := f.getLeaves(ctx, t, start)
		if err != nil {
			return fmt.Errorf("could not get leaves from %x: %w", start, err)
		}

		// End is checked client-side, not sent on the wire, because VerifyRangeProof
		// mishandles an empty response with a non-empty end.
		exhausted := batch.truncate(t.End())

		if err := t.OnLeaves(ctx, batch); err != nil {
			return err
		}

		if exhausted || !more {
			return t.OnFinish(ctx)
		}
		if len(batch.keys) == 0 {
			// more with no keys would loop forever.
			return errMoreWithoutKeys
		}
		start = nextRangeKey(batch.lastKey())
	}
}

// getLeaves fetches and proof-verifies the leaf range at start, reporting whether
// more leaves remain to the right.
func (f *LeafFetcher) getLeaves(ctx context.Context, t Task, start []byte) (LeafBatch, bool, error) {
	root := t.Root()
	req := &syncpb.GetLeafRequest{
		RootHash:    root.Bytes(),
		AccountHash: accountBytes(t.Account()),
		StartKey:    start,
		KeyLimit:    uint32(MaxLeavesLimit),
	}
	var more bool
	resp, err := f.client.Send(ctx, req,
		func() *syncpb.GetLeafResponse { return &syncpb.GetLeafResponse{} },
		func(resp *syncpb.GetLeafResponse) error {
			m, err := verifyLeaves(root, start, resp)
			if err != nil {
				f.log.Debug("invalid leaf response, re-requesting", zap.Error(err))
				return err
			}
			more = m
			return nil
		},
	)
	if err != nil {
		return LeafBatch{}, false, err
	}
	return LeafBatch{keys: resp.GetKeys(), vals: resp.GetValues()}, more, nil
}

// verifyLeaves range-proves resp against root and reports whether more leaves remain.
func verifyLeaves(root common.Hash, start []byte, resp *syncpb.GetLeafResponse) (bool, error) {
	keys, vals, proofVals := resp.GetKeys(), resp.GetValues(), resp.GetProofVals()
	if len(keys) > int(MaxLeavesLimit) {
		return false, fmt.Errorf("%w: got %d", errTooManyLeaves, len(keys))
	}
	if len(keys) == 0 && len(proofVals) == 0 {
		return false, errEmptyLeafResponse
	}

	// A whole-trie response carries no proof, so VerifyRangeProof asserts the keys
	// are the complete trie for root. Otherwise rebuild the proof, keyed by hash.
	var proof ethdb.Database
	if len(proofVals) > 0 {
		proof = rawdb.NewMemoryDatabase()
		defer proof.Close()
		for _, val := range proofVals {
			if err := proof.Put(crypto.Keccak256(val), val); err != nil {
				return false, err
			}
		}
	}

	// A nil start means the trie's beginning, which VerifyRangeProof wants zero-padded.
	firstKey := start
	if firstKey == nil && len(keys) > 0 {
		firstKey = make([]byte, len(keys[0]))
	}

	more, err := trie.VerifyRangeProof(root, firstKey, keys, vals, proof)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errInvalidRangeProof, err)
	}
	return more, nil
}

func accountBytes(account common.Hash) []byte {
	if account == (common.Hash{}) {
		return nil
	}
	return account.Bytes()
}
