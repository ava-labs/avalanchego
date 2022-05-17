// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
)

// atomicSyncer is used to sync the atomic trie from the network. The CallbackLeafSyncer
// is responsible for orchestrating the sync while atomicSyncer is responsible for maintaining
// the state of progress and writing the actual atomic trie to the trieDB.
type atomicSyncer struct {
	atomicTrie   *atomicTrie
	targetRoot   common.Hash
	targetHeight uint64

	// syncer is used to sync leaves from the network.
	syncer *syncclient.CallbackLeafSyncer

	// nextHeight is the height which key / values
	// are being inserted into [atomicTrie] for
	nextHeight uint64

	// nextCommit is the next height at which the atomic trie
	// should be committed.
	nextCommit uint64
}

// addZeros adds [common.HashLenth] zeros to [height] and returns the result as []byte
func addZeroes(height uint64) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, atomicKeyLength)}
	packer.PackLong(height)
	packer.PackFixedBytes(bytes.Repeat([]byte{0x00}, common.HashLength))
	return packer.Bytes
}

func newAtomicSyncer(client syncclient.LeafClient, atomicTrie *atomicTrie, targetRoot common.Hash, targetHeight uint64) *atomicSyncer {
	_, lastCommit := atomicTrie.LastCommitted()

	return &atomicSyncer{
		atomicTrie:   atomicTrie,
		targetRoot:   targetRoot,
		targetHeight: targetHeight,
		nextCommit:   lastCommit + atomicTrie.commitHeightInterval,
		nextHeight:   lastCommit + 1,
		syncer:       syncclient.NewCallbackLeafSyncer(client),
	}
}

// Start begins syncing the target atomic root.
func (s *atomicSyncer) Start(ctx context.Context) {
	s.syncer.Start(ctx, 1, &syncclient.LeafSyncTask{
		NodeType:      message.AtomicTrieNode,
		Root:          s.targetRoot,
		Start:         addZeroes(s.nextHeight),
		OnLeafs:       s.onLeafs,
		OnFinish:      s.onFinish,
		OnSyncFailure: s.onSyncFailure,
	})
}

// onLeafs is the callback for the leaf syncer, which will insert the key-value pairs into the trie.
func (s *atomicSyncer) onLeafs(_ common.Hash, keys [][]byte, values [][]byte) ([]*syncclient.LeafSyncTask, error) {
	for i, key := range keys {
		if len(key) != atomicKeyLength {
			return nil, fmt.Errorf("unexpected key len (%d) in atomic trie sync", len(key))
		}
		// key = height + blockchainID
		height := binary.BigEndian.Uint64(key[:wrappers.LongLen])

		// Commit the trie and update [nextCommit] if we are crossing a commit interval
		if height > s.nextCommit {
			if err := s.atomicTrie.commit(s.nextCommit); err != nil {
				return nil, err
			}
			if err := s.atomicTrie.db.Commit(); err != nil {
				return nil, err
			}
			s.nextCommit += s.atomicTrie.commitHeightInterval
		}

		if err := s.atomicTrie.trie.TryUpdate(key, values[i]); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// onFinish is called when sync for this trie is complete.
// commit the trie to disk and perform the final checks that we synced the target root correctly.
func (s *atomicSyncer) onFinish(_ common.Hash) error {
	// commit the trie on finish
	if err := s.atomicTrie.commit(s.targetHeight); err != nil {
		return err
	}
	if err := s.atomicTrie.db.Commit(); err != nil {
		return err
	}

	// the root of the trie should always match the targetRoot  since we already verified the proofs,
	// here we check the root mainly for correctness of the atomicTrie's pointers and it should never fail.
	root, _ := s.atomicTrie.LastCommitted()
	if s.targetRoot != root {
		return fmt.Errorf("synced root (%s) does not match expected (%s) for atomic trie ", root, s.targetRoot)
	}
	return nil
}

// onSyncFailure is a no-op since we flush progress to disk at the regular commit interval when syncing
// the atomic trie.
func (s *atomicSyncer) onSyncFailure(error) error {
	return nil
}

// Done returns a channel which produces any error that occurred during syncing or nil on success.
func (s *atomicSyncer) Done() <-chan error { return s.syncer.Done() }
