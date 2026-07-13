// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"golang.org/x/sync/errgroup"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const defaultLeafWorkers = 8

var (
	errRootRequired      = errors.New("root must be non-zero")
	errEmptyLeafResponse = errors.New("empty leaf response must include a proof")
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errInvalidRangeProof = errors.New("invalid range proof")
	errMoreWithoutKeys   = errors.New("more leaves reported but none returned")
	errRootMismatch      = errors.New("reconstructed root does not match target")
)

// task is one unit of leaf work the pool drives: a contiguous key range of a trie
// with callbacks for each verified batch and for completion. Implemented by [stateSegment].
type task interface {
	Root() common.Hash
	Account() common.Hash
	Start() []byte
	// End is the inclusive last key of the range, or nil for the whole trie.
	End() []byte
	OnLeaves(ctx context.Context, keys, vals [][]byte) error
	OnFinish(ctx context.Context) error
}

// callbackSyncer reconstructs tries with a pool of workers pulling tasks from a
// channel. Each worker walks its task's range left to right, verifying every batch
// in the fetch path ([getLeaves]) client-side rather than in the transport.
type callbackSyncer struct {
	client     *Client
	tasks      <-chan task
	numWorkers int
}

func newCallbackSyncer(client *Client, tasks <-chan task, numWorkers int) *callbackSyncer {
	if numWorkers <= 0 {
		numWorkers = defaultLeafWorkers
	}
	return &callbackSyncer{client: client, tasks: tasks, numWorkers: numWorkers}
}

// sync runs the workers until tasks is drained and closed, or ctx ends.
func (c *callbackSyncer) sync(ctx context.Context) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for range c.numWorkers {
		eg.Go(func() error { return c.workerLoop(egCtx) })
	}
	return eg.Wait()
}

// workerLoop processes tasks until the channel closes or ctx ends.
func (c *callbackSyncer) workerLoop(ctx context.Context) error {
	for {
		select {
		case t, ok := <-c.tasks:
			if !ok {
				return nil
			}
			if err := c.syncTask(ctx, t); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncTask requests the task's range left to right, truncating anything past End,
// and finishes when the range is exhausted or End is reached.
func (c *callbackSyncer) syncTask(ctx context.Context, t task) error {
	start := t.Start()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		keys, vals, more, err := getLeaves(ctx, c.client, t.Root(), t.Account(), start)
		if err != nil {
			return fmt.Errorf("could not get leaves from %x: %w", start, err)
		}

		// Truncate keys past End. End is checked client-side, not sent on the wire,
		// because VerifyRangeProof mishandles an empty response with a non-empty end.
		done := false
		if end := t.End(); end != nil && len(keys) > 0 {
			i := len(keys) - 1
			for ; i >= 0; i-- {
				if bytes.Compare(keys[i], end) <= 0 {
					break
				}
				done = true
			}
			keys, vals = keys[:i+1], vals[:i+1]
		}

		if err := t.OnLeaves(ctx, keys, vals); err != nil {
			return err
		}

		if done || !more {
			return t.OnFinish(ctx)
		}
		if len(keys) == 0 {
			// more with no keys would loop forever.
			return errMoreWithoutKeys
		}
		start = nextRangeKey(keys[len(keys)-1])
	}
}

// getLeaves requests the range at start, verifies the proof, scores the peer, and
// re-requests on any failure until ctx ends. It returns the leaves and whether more
// remain to the right.
func getLeaves(ctx context.Context, c *Client, root, account common.Hash, start []byte) ([][]byte, [][]byte, bool, error) {
	req := &syncpb.GetLeafRequest{
		RootHash:    root.Bytes(),
		AccountHash: accountBytes(account),
		StartKey:    start,
		KeyLimit:    uint32(MaxLeavesLimit),
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}

		resp := &syncpb.GetLeafResponse{}
		outcome, err := c.Send(ctx, req, resp)
		if err != nil {
			// Send already de-scored the peer, re-request from another.
			continue
		}

		more, err := verifyLeaves(root, start, resp)
		if err != nil {
			outcome.Failure()
			log.Debug("invalid leaf response, re-requesting", "err", err)
			continue
		}

		outcome.Success()
		return resp.GetKeys(), resp.GetValues(), more, nil
	}
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

	firstKey := start
	if firstKey == nil && len(keys) > 0 {
		firstKey = bytes.Repeat([]byte{0x00}, len(keys[0]))
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

// nextRangeKey returns the byte-incremented copy of k, the next range's start.
func nextRangeKey(k []byte) []byte {
	next := common.CopyBytes(k)
	incrementBytes(next)
	return next
}
