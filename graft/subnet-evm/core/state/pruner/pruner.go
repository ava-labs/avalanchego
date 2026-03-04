// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package pruner

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

const (
	// stateBloomFilePrefix is the filename prefix of state bloom filter.
	stateBloomFilePrefix = "statebloom"

	// stateBloomFileSuffix is the filename suffix of state bloom filter.
	stateBloomFileSuffix = "bf.gz"

	// stateBloomFileTempSuffix is the filename suffix of state bloom filter
	// while it is being written out to detect write aborts.
	stateBloomFileTempSuffix = ".tmp"

	// rangeCompactionThreshold is the minimal deleted entry number for
	// triggering range compaction. It's a quite arbitrary number but just
	// to avoid triggering range compaction because of small deletion.
	rangeCompactionThreshold = 100000
)

// Config includes all the configurations for pruning.
type Config struct {
	Datadir   string // The directory of the state database
	BloomSize uint64 // The Megabytes of memory allocated to bloom-filter
}

// Pruner is an offline tool to prune the stale state with the
// help of the snapshot. The workflow of pruner is very simple:
//
//   - iterate the snapshot, reconstruct the relevant state
//   - iterate the database, delete all other state entries which
//     don't belong to the target state and the genesis state
//
// It can take several hours(around 2 hours for mainnet) to finish
// the whole pruning work. It's recommended to run this offline tool
// periodically in order to release the disk usage and improve the
// disk read performance to some extent.
type Pruner struct {
	config      Config
	chainHeader *types.Header
	db          ethdb.Database
	stateBloom  *stateBloom
	snaptree    *snapshot.Tree
}

// NewPruner creates the pruner instance.
func NewPruner(db ethdb.Database, config Config) (*Pruner, error) {
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return nil, errors.New("failed to load head block")
	}
	// Offline pruning is only supported in legacy hash based scheme.
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)

	// Note: we refuse to start a pruning session unless the snapshot disk layer exists, which should prevent
	// us from ever needing to enter RecoverPruning in an invalid pruning session (a session where we do not have
	// the protected trie in the triedb and in the snapshot disk layer).

	snapconfig := snapshot.Config{
		CacheSize:  256,
		AsyncBuild: false,
		NoBuild:    true,
		SkipVerify: true,
	}
	snaptree, err := snapshot.New(snapconfig, db, triedb, headBlock.Hash(), headBlock.Root())
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot for pruning, must restart without offline pruning disabled to recover: %w", err) // The relevant snapshot(s) might not exist
	}
	// Sanitize the bloom filter size if it's too small.
	if config.BloomSize < 256 {
		log.Warn("Sanitizing bloomfilter size", "provided(MB)", config.BloomSize, "updated(MB)", 256)
		config.BloomSize = 256
	}
	stateBloom, err := newStateBloomWithSize(config.BloomSize)
	if err != nil {
		return nil, err
	}
	return &Pruner{
		config:      config,
		chainHeader: headBlock.Header(),
		db:          db,
		stateBloom:  stateBloom,
		snaptree:    snaptree,
	}, nil
}

func prune(maindb ethdb.Database, stateBloom *stateBloom, bloomPath string, start time.Time) error {
	// Delete all stale trie nodes in the disk. With the help of state bloom
	// the trie nodes(and codes) belong to the active state will be filtered
	// out. A very small part of stale tries will also be filtered because of
	// the false-positive rate of bloom filter. But the assumption is held here
	// that the false-positive is low enough(~0.05%). The probability of the
	// dangling node is the state root is super low. So the dangling nodes in
	// theory will never ever be visited again.
	var (
		skipped, count int
		size           common.StorageSize
		pstart         = time.Now()
		logged         = time.Now()
		batch          = maindb.NewBatch()
		iter           = maindb.NewIterator(nil, nil)
	)
	// We wrap iter.Release() in an anonymous function so that the [iter]
	// value captured is the value of [iter] at the end of the function as opposed
	// to incorrectly capturing the first iterator immediately.
	defer func() {
		iter.Release()
	}()

	for iter.Next() {
		key := iter.Key()

		// All state entries don't belong to specific state and genesis are deleted here
		// - trie node
		// - legacy contract code
		// - new-scheme contract code
		isCode, codeKey := rawdb.IsCodeKey(key)
		if len(key) == common.HashLength || isCode {
			checkKey := key
			if isCode {
				checkKey = codeKey
			}
			if stateBloom.Contain(checkKey) {
				skipped += 1
				continue
			}
			count += 1
			size += common.StorageSize(len(key) + len(iter.Value()))
			if err := batch.Delete(key); err != nil {
				return err
			}

			var eta time.Duration // Realistically will never remain uninited
			if done := binary.BigEndian.Uint64(key[:8]); done > 0 {
				var (
					left  = math.MaxUint64 - binary.BigEndian.Uint64(key[:8])
					speed = done/uint64(time.Since(pstart)/time.Millisecond+1) + 1 // +1s to avoid division by zero
				)
				eta = time.Duration(left/speed) * time.Millisecond
			}
			if time.Since(logged) > 8*time.Second {
				log.Info("Pruning state data", "nodes", count, "skipped", skipped, "size", size,
					"elapsed", common.PrettyDuration(time.Since(pstart)), "eta", common.PrettyDuration(eta))
				logged = time.Now()
			}
			// Recreate the iterator after every batch commit in order
			// to allow the underlying compactor to delete the entries.
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					return err
				}
				batch.Reset()

				iter.Release()
				iter = maindb.NewIterator(nil, key)
			}
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("failed to iterate db during pruning: %w", err)
	}
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return err
		}
		batch.Reset()
	}
	iter.Release()
	log.Info("Pruned state data", "nodes", count, "size", size, "elapsed", common.PrettyDuration(time.Since(pstart)))

	// Write marker to DB to indicate offline pruning finished successfully. We write before calling os.RemoveAll
	// to guarantee that if the node dies midway through pruning, then this will run during RecoverPruning.
	if err := customrawdb.WriteOfflinePruning(maindb); err != nil {
		return fmt.Errorf("failed to write offline pruning success marker: %w", err)
	}

	// Delete the state bloom, it marks the entire pruning procedure is
	// finished. If any crashes or manual exit happens before this,
	// `RecoverPruning` will pick it up in the next restarts to redo all
	// the things.
	if err := os.RemoveAll(bloomPath); err != nil {
		return fmt.Errorf("failed to remove bloom filter from disk: %w", err)
	}

	// Start compactions, will remove the deleted data from the disk immediately.
	// Note for small pruning, the compaction is skipped.
	if count >= rangeCompactionThreshold {
		cstart := time.Now()
		for b := 0x00; b <= 0xf0; b += 0x10 {
			var (
				start = []byte{byte(b)}
				end   = []byte{byte(b + 0x10)}
			)
			if b == 0xf0 {
				end = nil
			}
			log.Info("Compacting database", "range", fmt.Sprintf("%#x-%#x", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
			if err := maindb.Compact(start, end); err != nil {
				log.Error("Database compaction failed", "error", err)
				return err
			}
		}
		log.Info("Database compaction finished", "elapsed", common.PrettyDuration(time.Since(cstart)))
	}
	log.Info("State pruning successful", "pruned", size, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// Prune deletes all historical state nodes except the nodes belong to the
// specified state version. If user doesn't specify the state version, use
// the bottom-most snapshot diff layer as the target.
func (p *Pruner) Prune(root common.Hash) error {
	// If the state bloom filter is already committed previously,
	// reuse it for pruning instead of generating a new one. It's
	// mandatory because a part of state may already be deleted,
	// the recovery procedure is necessary.
	_, stateBloomRoot, err := findBloomFilter(p.config.Datadir)
	if err != nil {
		return err
	}
	if stateBloomRoot != (common.Hash{}) {
		return RecoverPruning(p.config.Datadir, p.db)
	}

	// If the target state root is not specified, return a fatal error.
	if root == (common.Hash{}) {
		return fmt.Errorf("cannot prune with an empty root: %s", root)
	}
	// Ensure the root is really present. The weak assumption
	// is the presence of root can indicate the presence of the
	// entire trie.
	if !rawdb.HasLegacyTrieNode(p.db, root) {
		return fmt.Errorf("associated state[%x] is not present", root)
	} else {
		log.Info("Selecting last accepted block root as the pruning target", "root", root)
	}

	// Traverse the target state, re-construct the whole state trie and
	// commit to the given bloom filter.
	start := time.Now()
	if err := snapshot.GenerateTrie(p.snaptree, root, p.db, p.stateBloom); err != nil {
		return err
	}
	// Traverse the genesis, put all genesis state entries into the
	// bloom filter too.
	if err := extractGenesis(p.db, p.stateBloom); err != nil {
		return err
	}
	filterName := bloomFilterName(p.config.Datadir, root)

	log.Info("Writing state bloom to disk", "name", filterName)
	if err := p.stateBloom.Commit(filterName, filterName+stateBloomFileTempSuffix); err != nil {
		return err
	}
	log.Info("State bloom filter committed", "name", filterName)
	return prune(p.db, p.stateBloom, filterName, start)
}

// RecoverPruning will resume the pruning procedure during the system restart.
// This function is used in this case: user tries to prune state data, but the
// system was interrupted midway because of crash or manual-kill. In this case
// if the bloom filter for filtering active state is already constructed, the
// pruning can be resumed. What's more if the bloom filter is constructed, the
// pruning **has to be resumed**. Otherwise a lot of dangling nodes may be left
// in the disk.
func RecoverPruning(datadir string, db ethdb.Database) error {
	stateBloomPath, stateBloomRoot, err := findBloomFilter(datadir)
	if err != nil {
		return err
	}
	if stateBloomPath == "" {
		return nil // nothing to recover
	}
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return errors.New("failed to load head block")
	}
	stateBloom, err := NewStateBloomFromDisk(stateBloomPath)
	if err != nil {
		return err
	}
	log.Info("Loaded state bloom filter", "path", stateBloomPath)

	// All the state roots of the middle layers should be forcibly pruned,
	// otherwise the dangling state will be left.
	if stateBloomRoot != headBlock.Root() {
		return fmt.Errorf("cannot recover pruning to state bloom root: %s, with head block root: %s", stateBloomRoot, headBlock.Root())
	}

	return prune(db, stateBloom, stateBloomPath, time.Now())
}

// extractGenesis loads the genesis state and commits all the state entries
// into the given bloomfilter.
func extractGenesis(db ethdb.Database, stateBloom *stateBloom) error {
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return errors.New("missing genesis hash")
	}
	genesis := rawdb.ReadBlock(db, genesisHash, 0)
	if genesis == nil {
		return errors.New("missing genesis block")
	}
	t, err := trie.NewStateTrie(trie.StateTrieID(genesis.Root()), triedb.NewDatabase(db, triedb.HashDefaults))
	if err != nil {
		return err
	}
	accIter, err := t.NodeIterator(nil)
	if err != nil {
		return err
	}
	for accIter.Next(true) {
		hash := accIter.Hash()

		// Embedded nodes don't have hash.
		if hash != (common.Hash{}) {
			stateBloom.Put(hash.Bytes(), nil)
		}
		// If it's a leaf node, yes we are touching an account,
		// dig into the storage trie further.
		if accIter.Leaf() {
			var acc types.StateAccount
			if err := rlp.DecodeBytes(accIter.LeafBlob(), &acc); err != nil {
				return err
			}
			if acc.Root != types.EmptyRootHash {
				id := trie.StorageTrieID(genesis.Root(), common.BytesToHash(accIter.LeafKey()), acc.Root)
				storageTrie, err := trie.NewStateTrie(id, triedb.NewDatabase(db, triedb.HashDefaults))
				if err != nil {
					return err
				}
				storageIter, err := storageTrie.NodeIterator(nil)
				if err != nil {
					return err
				}
				for storageIter.Next(true) {
					hash := storageIter.Hash()
					if hash != (common.Hash{}) {
						stateBloom.Put(hash.Bytes(), nil)
					}
				}
				if storageIter.Error() != nil {
					return storageIter.Error()
				}
			}
			if !bytes.Equal(acc.CodeHash, types.EmptyCodeHash.Bytes()) {
				stateBloom.Put(acc.CodeHash, nil)
			}
		}
	}
	return accIter.Error()
}

func bloomFilterName(datadir string, hash common.Hash) string {
	return filepath.Join(datadir, fmt.Sprintf("%s.%s.%s", stateBloomFilePrefix, hash.Hex(), stateBloomFileSuffix))
}

func isBloomFilter(filename string) (bool, common.Hash) {
	filename = filepath.Base(filename)
	if strings.HasPrefix(filename, stateBloomFilePrefix) && strings.HasSuffix(filename, stateBloomFileSuffix) {
		return true, common.HexToHash(filename[len(stateBloomFilePrefix)+1 : len(filename)-len(stateBloomFileSuffix)-1])
	}
	return false, common.Hash{}
}

func findBloomFilter(datadir string) (string, common.Hash, error) {
	var (
		stateBloomPath string
		stateBloomRoot common.Hash
	)
	if err := filepath.Walk(datadir, func(path string, info os.FileInfo, err error) error {
		if info != nil && !info.IsDir() {
			ok, root := isBloomFilter(path)
			if ok {
				stateBloomPath = path
				stateBloomRoot = root
			}
		}
		return nil
	}); err != nil {
		return "", common.Hash{}, err
	}
	return stateBloomPath, stateBloomRoot, nil
}
