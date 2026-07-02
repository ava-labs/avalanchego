// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

package eth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/eth/tracers"
	"github.com/ava-labs/avalanchego/graft/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/firewood-go-ethhash/ffi"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

// noopReleaser is returned in case there is no operation expected
// for releasing state.
var noopReleaser = tracers.StateReleaseFunc(func() {})

func (eth *Ethereum) hashState(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (statedb *state.StateDB, release tracers.StateReleaseFunc, err error) {
	reexec = 0 // Do not support re-executing historical blocks to grab state
	var (
		current  *types.Block
		database state.Database
		tdb      *triedb.Database
		report   = true
		origin   = block.NumberU64()
	)
	// The state is only for reading purposes, check the state presence in
	// live database.
	if readOnly {
		// The state is available in live database, create a reference
		// on top to prevent garbage collection and return a release
		// function to deref it.
		if statedb, err = eth.blockchain.StateAt(block.Root()); err == nil {
			eth.blockchain.TrieDB().Reference(block.Root(), common.Hash{})
			return statedb, func() {
				eth.blockchain.TrieDB().Dereference(block.Root())
			}, nil
		}
	}
	// The state is both for reading and writing, or it's unavailable in disk,
	// try to construct/recover the state over an ephemeral trie.Database for
	// isolating the live one.
	if base != nil {
		if preferDisk {
			// Create an ephemeral trie.Database for isolating the live one. Otherwise
			// the internal junks created by tracing will be persisted into the disk.
			// TODO(rjl493456442), clean cache is disabled to prevent memory leak,
			// please re-enable it for better performance.
			database = extstate.NewDatabaseWithConfig(eth.chainDb, triedb.HashDefaults)
			if statedb, err = state.New(block.Root(), database, nil); err == nil {
				log.Info("Found disk backend for state trie", "root", block.Root(), "number", block.Number())
				return statedb, noopReleaser, nil
			}
		}
		// The optional base statedb is given, mark the start point as parent block
		statedb, database, tdb, report = base, base.Database(), base.Database().TrieDB(), false
		current = eth.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1)
	} else {
		// Otherwise, try to reexec blocks until we find a state or reach our limit
		current = block

		// Create an ephemeral trie.Database for isolating the live one. Otherwise
		// the internal junks created by tracing will be persisted into the disk.
		// TODO(rjl493456442), clean cache is disabled to prevent memory leak,
		// please re-enable it for better performance.
		tdb = triedb.NewDatabase(eth.chainDb, triedb.HashDefaults)
		database = extstate.NewDatabaseWithNodeDB(eth.chainDb, tdb)

		// If we didn't check the live database, do check state over ephemeral database,
		// otherwise we would rewind past a persisted block (specific corner case is
		// chain tracing from the genesis).
		if !readOnly {
			statedb, err = state.New(current.Root(), database, nil)
			if err == nil {
				return statedb, noopReleaser, nil
			}
		}
		// Database does not have the state for the given block, try to regenerate
		for i := uint64(0); i < reexec; i++ {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if current.NumberU64() == 0 {
				return nil, nil, errors.New("genesis state is missing")
			}
			parent := eth.blockchain.GetBlock(current.ParentHash(), current.NumberU64()-1)
			if parent == nil {
				return nil, nil, fmt.Errorf("missing block %v %d", current.ParentHash(), current.NumberU64()-1)
			}
			current = parent

			statedb, err = state.New(current.Root(), database, nil)
			if err == nil {
				break
			}
		}
		if err != nil {
			switch err.(type) {
			case *trie.MissingNodeError:
				return nil, nil, fmt.Errorf("required historical state unavailable (reexec=%d)", reexec)
			default:
				return nil, nil, err
			}
		}
	}
	// State is available at historical point, re-execute the blocks on top for
	// the desired state.
	var (
		start  = time.Now()
		logged time.Time
		parent common.Hash
	)
	for current.NumberU64() < origin {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second && report {
			log.Info("Regenerating historical state", "block", current.NumberU64()+1, "target", origin, "remaining", origin-current.NumberU64()-1, "elapsed", time.Since(start))
			logged = time.Now()
		}
		// Retrieve the next block to regenerate and process it
		parentHeader := current.Header()
		next := current.NumberU64() + 1
		if current = eth.blockchain.GetBlockByNumber(next); current == nil {
			return nil, nil, fmt.Errorf("block #%d not found", next)
		}
		_, _, _, err := eth.blockchain.Processor().Process(current, parentHeader, statedb, vm.Config{})
		if err != nil {
			return nil, nil, fmt.Errorf("processing block %d failed: %v", current.NumberU64(), err)
		}
		// Finalize the state so any modifications are written to the trie
		root, err := statedb.Commit(current.NumberU64(), eth.blockchain.Config().IsEIP158(current.Number()))
		if err != nil {
			return nil, nil, fmt.Errorf("stateAtBlock commit failed, number %d root %v: %w",
				current.NumberU64(), current.Root().Hex(), err)
		}
		statedb, err = state.New(root, database, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("state reset after block %d failed: %v", current.NumberU64(), err)
		}
		// Hold the state reference and also drop the parent state
		// to prevent accumulating too many nodes in memory.
		tdb.Reference(root, common.Hash{})
		if parent != (common.Hash{}) {
			tdb.Dereference(parent)
		}
		parent = root
	}
	if report {
		_, nodes, imgs := tdb.Size() // all memory is contained within the nodes return in hashdb
		log.Info("Historical state regenerated", "block", current.NumberU64(), "elapsed", time.Since(start), "nodes", nodes, "preimages", imgs)
	}
	return statedb, func() { tdb.Dereference(block.Root()) }, nil
}

// This is compatible with both PathDB and FirewoodDB schemes.
func (eth *Ethereum) pathState(block *types.Block) (*state.StateDB, func(), error) {
	// Check if the requested state is available in the live chain.
	statedb, err := eth.blockchain.StateAt(block.Root())
	if err == nil {
		return statedb, noopReleaser, nil
	}
	// TODO historic state is not supported in path-based scheme.
	// Fully archive node in pbss will be implemented by relying
	// on state history, but needs more work on top.
	return nil, nil, errors.New("historical state not available in path scheme yet")
}

// firewoodState reconstructs the state at the requested block (`header`) by
// walking back to a persisted revision or genesis, then re-executing blocks
// forward.
//
// The walk-back is bounded by `reexec`. If no persisted revision or genesis is
// found within `reexec` blocks of the requested block, this returns an error.
func (eth *Ethereum) firewoodState(ctx context.Context, header *types.Header, reexec uint64) (_ *state.StateDB, _ tracers.StateReleaseFunc, finalErr error) {
	// Fast path: state is available directly.
	if statedb, err := eth.blockchain.StateAt(header.Root); err == nil {
		return statedb, noopReleaser, nil
	}

	// Get the Firewood TrieDB.
	fwDB, ok := eth.blockchain.TrieDB().Backend().(*firewood.TrieDB)
	if !ok {
		return nil, nil, errors.New("expected Firewood backend for historical state reconstruction")
	}

	var (
		current        = header
		reachedGenesis = false
	)

	for i := uint64(0); i < reexec; i++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if eth.blockchain.HasState(current.Root) {
			break
		}
		if current.Number.Uint64() == 0 {
			reachedGenesis = true
			break
		}
		parent := eth.blockchain.GetHeader(current.ParentHash, current.Number.Uint64()-1)
		if parent == nil {
			return nil, nil, fmt.Errorf("missing block %v %d", current.ParentHash, current.Number.Uint64()-1)
		}
		current = parent
	}

	var (
		release tracers.StateReleaseFunc
		recon   *ffi.Reconstructed
	)

	// Establish the base reconstructed view and the starting point for replay.
	if reachedGenesis {
		// Genesis state is not in Firewood; rebuild it from the genesis spec.
		genesisRecon, err := eth.reconstructGenesis(fwDB)
		if err != nil {
			return nil, nil, err
		}

		// Use the canonical genesis root and header as the starting point for
		// optional re-execution.
		genesisBlock := eth.blockchain.GetBlockByNumber(0)
		if genesisBlock == nil {
			if dropErr := genesisRecon.Drop(); dropErr != nil {
				log.Warn("Failed to drop reconstructed view", "err", dropErr)
			}
			return nil, nil, errors.New("genesis block not found")
		}
		recon = genesisRecon
		current = genesisBlock.Header()
	} else {
		if !eth.blockchain.HasState(current.Root) {
			return nil, nil, fmt.Errorf("no persisted state found within %d blocks", reexec)
		}

		// Create the initial reconstructed revision from the base revision.
		var err error
		recon, err = reconstructRevision(fwDB, current.Root)
		if err != nil {
			return nil, nil, err
		}
	}
	release = func() { recon.Drop() }

	defer func() {
		if finalErr != nil {
			release()
		}
	}()

	// Build a replay-mode state database backed by the reconstructed view. The
	// reconstructed revision is mutated in place across blocks, so a single database
	// remains valid for the whole replay. Root hashing is deferred until after replay,
	// when the target root is validated once against the requested header.
	replayTrieDB := firewood.NewReconstructedTrieDB(fwDB, recon, false /* computeRootOnHash */)
	cache, err := state.New(current.Root, extstate.NewDatabaseWithNodeDB(eth.chainDb, replayTrieDB), nil)
	if err != nil {
		return nil, nil, err
	}

	// Re-execute blocks forward from current+1 to the target block.
	for current.Number.Uint64() < header.Number.Uint64() {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		next := current.Number.Uint64() + 1
		nextBlock := eth.blockchain.GetBlockByNumber(next)
		if nextBlock == nil {
			return nil, nil, fmt.Errorf("block %d not found", next)
		}

		_, _, _, err := eth.blockchain.Processor().Process(nextBlock, current, cache, vm.Config{})
		if err != nil {
			return nil, nil, fmt.Errorf("processing block %d: %w", next, err)
		}

		// Flush the block's writes into the reconstructed view. The returned root
		// is ignored; the final root is computed from recon once after replay.
		cache.IntermediateRoot(eth.blockchain.Config().IsEIP158(nextBlock.Number()))
		current = nextBlock.Header()
	}

	// Root computation was deferred during replay; compute it once now and
	// validate it against the requested header.
	if replayRoot := common.Hash(recon.Root()); replayRoot != header.Root {
		return nil, nil, fmt.Errorf(
			"state root mismatch at block %d: got %s, want %s",
			header.Number.Uint64(),
			replayRoot.Hex(),
			header.Root.Hex(),
		)
	}

	returnTrieDB := firewood.NewReconstructedTrieDB(fwDB, recon, true /* computeRootOnHash */)
	cache, err = state.New(header.Root, extstate.NewDatabaseWithNodeDB(eth.chainDb, returnTrieDB), nil)
	if err != nil {
		return nil, nil, err
	}

	return cache, release, nil
}

// reconstructRevision opens the Firewood revision at root and returns a
// reconstructed view seeded from it.
func reconstructRevision(fwDB *firewood.TrieDB, root common.Hash) (*ffi.Reconstructed, error) {
	rev, err := fwDB.Firewood.Revision(ffi.Hash(root))
	if err != nil {
		return nil, fmt.Errorf("opening revision at %s: %w", root.Hex(), err)
	}
	recon, err := rev.Reconstruct(nil)
	if dropErr := rev.Drop(); dropErr != nil {
		log.Warn("Failed to drop revision", "root", root.Hex(), "err", dropErr)
	}
	if err != nil {
		return nil, fmt.Errorf("reconstructing revision at %s: %w", root.Hex(), err)
	}
	return recon, nil
}

// reconstructGenesis builds a Firewood reconstructed revision populated with
// the committed genesis state.
func (eth *Ethereum) reconstructGenesis(fwDB *firewood.TrieDB) (*ffi.Reconstructed, error) {
	recon, err := reconstructRevision(fwDB, types.EmptyRootHash)
	if err != nil {
		return nil, err
	}

	// Commit the genesis allocation into the reconstructed view. Root hashing is enabled
	// so the commit produces the canonical genesis root. The in-memory database is a throwaway;
	// the reconstructed trie serves all reads and writes.
	genesisTrieDB := firewood.NewReconstructedTrieDB(fwDB, recon, true /* computeRootOnHash */)
	if _, err := eth.config.Genesis.Commit(rawdb.NewMemoryDatabase(), genesisTrieDB); err != nil {
		if dropErr := recon.Drop(); dropErr != nil {
			log.Warn("Failed to drop reconstructed view", "err", dropErr)
		}
		return nil, err
	}
	return recon, nil
}

// stateAtBlock retrieves the state database associated with a certain block.
// If no state is locally available for the given block, a number of blocks
// are attempted to be reexecuted to generate the desired state. The optional
// base layer statedb can be provided which is regarded as the statedb of the
// parent block.
//
// An additional release function will be returned if the requested state is
// available. Release is expected to be invoked when the returned state is no
// longer needed. Its purpose is to prevent resource leaking. Though it can be
// noop in some cases.
//
// Parameters:
//   - block:      The block for which we want the state(state = block.Root)
//   - reexec:     The maximum number of blocks to reprocess trying to obtain the desired state
//   - base:       If the caller is tracing multiple blocks, the caller can provide the parent
//     state continuously from the callsite.
//   - readOnly:   If true, then the live 'blockchain' state database is used. No mutation should
//     be made from caller, e.g. perform Commit or other 'save-to-disk' changes.
//     Otherwise, the trash generated by caller may be persisted permanently.
//   - preferDisk: This arg can be used by the caller to signal that even though the 'base' is
//     provided, it would be preferable to start from a fresh state, if we have it
//     on disk.
func (eth *Ethereum) stateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (statedb *state.StateDB, release tracers.StateReleaseFunc, err error) {
	switch eth.blockchain.CacheConfig().StateScheme {
	case customrawdb.FirewoodScheme:
		return eth.firewoodState(ctx, block.Header(), reexec)
	case rawdb.PathScheme:
		return eth.pathState(block)
	default:
		return eth.hashState(ctx, block, reexec, base, readOnly, preferDisk)
	}
}

// stateAtTransaction returns the execution environment of a certain transaction.
func (eth *Ethereum) stateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	// Short circuit if it's genesis block.
	if block.NumberU64() == 0 {
		return nil, vm.BlockContext{}, nil, nil, errors.New("no transaction in genesis")
	}
	// Create the parent state database
	parent := eth.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("parent %#x not found", block.ParentHash())
	}
	// Lookup the statedb of parent block from the live database,
	// otherwise regenerate it on the flight.
	statedb, release, err := eth.StateAtNextBlock(ctx, parent, block, reexec, nil, true, false)
	if err != nil {
		return nil, vm.BlockContext{}, nil, nil, err
	}
	if txIndex == 0 && len(block.Transactions()) == 0 {
		return nil, vm.BlockContext{}, statedb, release, nil
	}
	// Recompute transactions up to the target index.
	signer := types.MakeSigner(eth.blockchain.Config(), block.Number(), block.Time())
	for idx, tx := range block.Transactions() {
		// Assemble the transaction call message and return if the requested offset
		msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
		txContext := core.NewEVMTxContext(msg)
		context := core.NewEVMBlockContext(block.Header(), eth.blockchain, nil)
		if idx == txIndex {
			return msg, context, statedb, release, nil
		}
		// Not yet the searched for transaction, execute on top of the current state
		vmenv := vm.NewEVM(context, txContext, statedb, eth.blockchain.Config(), vm.Config{})
		statedb.SetTxContext(tx.Hash(), idx)
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(tx.Gas())); err != nil {
			return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction %#x failed: %v", tx.Hash(), err)
		}
		// Ensure any modifications are committed to the state
		// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
		statedb.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))
	}
	return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction index %d out of range for block %#x", txIndex, block.Hash())
}

// StateAtNextBlock is a helper function that returns the state at the next block.
// It wraps StateAtBlock and handles the case where Upgrades are applied to the
// next block.
// This is different than using StateAtBlock with [nextBlock] because it will
// apply the upgrades to the [parent] state before returning it.
func (eth *Ethereum) StateAtNextBlock(ctx context.Context, parent *types.Block, nextBlock *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	// Get state for [parent]
	statedb, release, err := eth.stateAtBlock(ctx, parent, reexec, base, readOnly, preferDisk)
	if err != nil {
		return nil, nil, err
	}

	// Apply upgrades here for the [nextBlock]
	blockContext := core.NewBlockContext(nextBlock.Number(), nextBlock.Time())
	err = core.ApplyUpgrades(eth.blockchain.Config(), &parent.Header().Time, blockContext, statedb)
	if err != nil {
		release()
		return nil, nil, err
	}

	return statedb, release, nil
}
