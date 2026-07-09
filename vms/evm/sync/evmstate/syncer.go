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

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	errRootRequired      = errors.New("root must be non-zero")
	errEmptyLeafResponse = errors.New("empty leaf response must include a proof")
	errTooManyLeaves     = errors.New("more leaves returned than requested")
	errInvalidRangeProof = errors.New("invalid range proof")
	errMoreWithoutKeys   = errors.New("more leaves reported but none returned")
	errRootMismatch      = errors.New("reconstructed root does not match target")
)

// Syncer reconstructs the trie at root, rebuilding range-proof-verified leaves
// into db with a [trie.StackTrie] and checking the committed root matches.
type Syncer struct {
	client  *Client
	db      ethdb.KeyValueStore
	root    common.Hash
	account common.Hash
}

// NewSyncer returns a [Syncer] for the trie at root. account is empty for the
// account trie, non-empty for a storage trie.
func NewSyncer(c *Client, db ethdb.KeyValueStore, root, account common.Hash) (*Syncer, error) {
	if root == (common.Hash{}) {
		return nil, errRootRequired
	}
	return &Syncer{client: c, db: db, root: root, account: account}, nil
}

// Sync walks the key range left to right into a StackTrie, then commits and
// fails if the reconstructed root does not match.
func (s *Syncer) Sync(ctx context.Context) error {
	batch := s.db.NewBatch()
	stack := trie.NewStackTrie(&trie.StackTrieOptions{
		Writer: func(path []byte, hash common.Hash, blob []byte) {
			rawdb.WriteTrieNode(batch, s.account, path, hash, blob, rawdb.HashScheme)
		},
	})

	// TODO(powerslider): segment large tries and resume across restarts.
	var start []byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		keys, vals, more, err := getLeafs(ctx, s.client, s.root, s.account, start)
		if err != nil {
			return fmt.Errorf("could not get leaves from %x: %w", start, err)
		}

		// TODO(powerslider): recurse into storage tries and code.
		for i, k := range keys {
			if err := stack.Update(k, vals[i]); err != nil {
				return err
			}
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}

		if more {
			if len(keys) == 0 {
				// more with no keys would loop forever.
				return errMoreWithoutKeys
			}
			start = nextRangeKey(keys[len(keys)-1])
			continue
		}

		if root := stack.Commit(); root != s.root {
			return fmt.Errorf("%w: got %x want %x", errRootMismatch, root, s.root)
		}
		return batch.Write()
	}
}

// getLeafs requests the range starting at start, verifies the range proof,
// scores the peer, and re-requests on any network or verification failure until
// ctx ends. It returns the leaves and whether more remain to the right.
func getLeafs(ctx context.Context, c *Client, root, account common.Hash, start []byte) ([][]byte, [][]byte, bool, error) {
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

		more, err := verifyLeafs(root, start, resp)
		if err != nil {
			outcome.Failure()
			log.Debug("invalid leaf response, re-requesting", "err", err)
			continue
		}

		outcome.Success()
		return resp.GetKeys(), resp.GetValues(), more, nil
	}
}

// verifyLeafs range-proves resp against root and reports whether more leaves
// remain to the right of the last returned key.
func verifyLeafs(root common.Hash, start []byte, resp *syncpb.GetLeafResponse) (bool, error) {
	keys, vals, proofVals := resp.GetKeys(), resp.GetValues(), resp.GetProofVals()
	if len(keys) > int(MaxLeavesLimit) {
		return false, fmt.Errorf("%w: got %d", errTooManyLeaves, len(keys))
	}
	if len(keys) == 0 && len(proofVals) == 0 {
		return false, errEmptyLeafResponse
	}

	// A whole-trie response carries no proof, VerifyRangeProof then asserts the
	// keys are the complete trie for root. Otherwise rebuild the proof from its
	// nodes, keyed by hash.
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

// nextRangeKey returns the byte-incremented copy of k, the start of the next
// range to fetch.
func nextRangeKey(k []byte) []byte {
	next := common.CopyBytes(k)
	incrementBytes(next)
	return next
}
