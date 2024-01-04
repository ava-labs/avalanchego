// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ethereum/go-ethereum/log"
)

var repairedKey = []byte("atomicTrieHasBonusBlocks")

// TODO: Remove this after the Durango
// repairAtomicTrie applies the bonus blocks to the atomic trie so all nodes
// can have a canonical atomic trie.
// Initially, bonus blocks were not indexed into the atomic trie. However, a
// regression caused some nodes to index these blocks.
// Returns the number of heights repaired.
func (a *atomicTrie) repairAtomicTrie(bonusBlockIDs map[uint64]ids.ID, bonusBlocks map[uint64]*types.Block) (int, error) {
	done, err := a.metadataDB.Has(repairedKey)
	if err != nil {
		return 0, err
	}
	if done {
		return 0, nil
	}

	root, lastCommitted := a.LastCommitted()
	tr, err := a.OpenTrie(root)
	if err != nil {
		return 0, err
	}

	heightsRepaired := 0
	puts, removes := 0, 0
	for height, block := range bonusBlocks {
		if height > lastCommitted {
			// Avoid applying the repair to heights not yet committed
			continue
		}

		blockID, ok := bonusBlockIDs[height]
		if !ok {
			// Should not happen since we enforce the keys of bonusBlockIDs
			// to be the same as the keys of bonusBlocks on init.
			return 0, fmt.Errorf("missing block ID for height %d", height)
		}
		txs, err := ExtractAtomicTxs(block.ExtData(), false, a.codec)
		if err != nil {
			return 0, fmt.Errorf("failed to extract atomic txs from bonus block at height %d: %w", height, err)
		}
		log.Info("repairing atomic trie", "height", height, "block", blockID, "txs", len(txs))
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return 0, err
		}
		if err := a.UpdateTrie(tr, height, combinedOps); err != nil {
			return 0, err
		}
		for _, op := range combinedOps {
			puts += len(op.PutRequests)
			removes += len(op.RemoveRequests)
		}
		heightsRepaired++
	}
	newRoot, nodes := tr.Commit(false)
	if nodes != nil {
		if err := a.trieDB.Update(newRoot, types.EmptyRootHash, trienode.NewWithNodeSet(nodes)); err != nil {
			return 0, err
		}
		if err := a.commit(lastCommitted, newRoot); err != nil {
			return 0, err
		}
	}

	// Putting either true or false are both considered repaired since we check
	// for the presence of the key to skip the repair.
	if database.PutBool(a.metadataDB, repairedKey, true); err != nil {
		return 0, err
	}
	log.Info(
		"repaired atomic trie", "originalRoot", root, "newRoot", newRoot,
		"heightsRepaired", heightsRepaired, "puts", puts, "removes", removes,
	)
	return heightsRepaired, nil
}
