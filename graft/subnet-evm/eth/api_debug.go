// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
// Copyright 2023 The go-ethereum Authors
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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/subnet-evm/internal/ethapi"
	"github.com/ava-labs/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/subnet-evm/rpc"
)

var errFirewoodNotSupported = errors.New("firewood triedb scheme does not yet support this operation")

// DebugAPI is the collection of Ethereum full node APIs for debugging the
// protocol.
type DebugAPI struct {
	eth *Ethereum
}

// NewDebugAPI creates a new DebugAPI instance.
func NewDebugAPI(eth *Ethereum) *DebugAPI {
	return &DebugAPI{eth: eth}
}

// DumpBlock retrieves the entire state of the database at a given block.
func (api *DebugAPI) DumpBlock(blockNr rpc.BlockNumber) (state.Dump, error) {
	opts := &state.DumpConfig{
		OnlyWithAddresses: true,
		Max:               AccountRangeMaxResults, // Sanity limit over RPC
	}
	var header *types.Header
	if blockNr.IsAccepted() {
		if api.eth.APIBackend.isLatestAndAllowed(blockNr) {
			header = api.eth.blockchain.CurrentHeader()
		} else {
			header = api.eth.LastAcceptedBlock().Header()
		}
	} else {
		block := api.eth.blockchain.GetBlockByNumber(uint64(blockNr))
		if block == nil {
			return state.Dump{}, fmt.Errorf("block #%d not found", blockNr)
		}
		header = block.Header()
	}
	if header == nil {
		return state.Dump{}, fmt.Errorf("block #%d not found", blockNr)
	}
	stateDb, err := api.eth.BlockChain().StateAt(header.Root)
	if err != nil {
		return state.Dump{}, err
	}
	return stateDb.RawDump(opts), nil
}

// Preimage is a debug API function that returns the preimage for a sha3 hash, if known.
func (api *DebugAPI) Preimage(ctx context.Context, hash common.Hash) (hexutil.Bytes, error) {
	if preimage := rawdb.ReadPreimage(api.eth.ChainDb(), hash); preimage != nil {
		return preimage, nil
	}
	return nil, errors.New("unknown preimage")
}

// GetBadBlocks returns a list of the last 'bad blocks' that the client has seen on the network
// and returns them as a JSON list of block hashes.
func (api *DebugAPI) GetBadBlocks(ctx context.Context) ([]*ethapi.BadBlockArgs, error) {
	internalAPI := ethapi.NewBlockChainAPI(api.eth.APIBackend)
	return internalAPI.GetBadBlocks(ctx)
}

// AccountRangeMaxResults is the maximum number of results to be returned per call
const AccountRangeMaxResults = 256

// AccountRange enumerates all accounts in the given block and start point in paging request
func (api *DebugAPI) AccountRange(blockNrOrHash rpc.BlockNumberOrHash, start hexutil.Bytes, maxResults int, nocode, nostorage, incompletes bool) (state.Dump, error) {
	var stateDb *state.StateDB
	var err error

	if number, ok := blockNrOrHash.Number(); ok {
		var header *types.Header
		if number.IsAccepted() {
			if api.eth.APIBackend.isLatestAndAllowed(number) {
				header = api.eth.blockchain.CurrentHeader()
			} else {
				header = api.eth.LastAcceptedBlock().Header()
			}
		} else {
			block := api.eth.blockchain.GetBlockByNumber(uint64(number))
			if block == nil {
				return state.Dump{}, fmt.Errorf("block #%d not found", number)
			}
			header = block.Header()
		}
		if header == nil {
			return state.Dump{}, fmt.Errorf("block #%d not found", number)
		}
		stateDb, err = api.eth.BlockChain().StateAt(header.Root)
		if err != nil {
			return state.Dump{}, err
		}
	} else if hash, ok := blockNrOrHash.Hash(); ok {
		block := api.eth.blockchain.GetBlockByHash(hash)
		if block == nil {
			return state.Dump{}, fmt.Errorf("block %s not found", hash.Hex())
		}
		stateDb, err = api.eth.BlockChain().StateAt(block.Root())
		if err != nil {
			return state.Dump{}, err
		}
	} else {
		return state.Dump{}, errors.New("either block number or block hash must be specified")
	}

	opts := &state.DumpConfig{
		SkipCode:          nocode,
		SkipStorage:       nostorage,
		OnlyWithAddresses: !incompletes,
		Start:             start,
		Max:               uint64(maxResults),
	}
	if maxResults > AccountRangeMaxResults || maxResults <= 0 {
		opts.Max = AccountRangeMaxResults
	}
	return stateDb.RawDump(opts), nil
}

// StorageRangeResult is the result of a debug_storageRangeAt API call.
type StorageRangeResult struct {
	Storage storageMap   `json:"storage"`
	NextKey *common.Hash `json:"nextKey"` // nil if Storage includes the last key in the trie.
}

type storageMap map[common.Hash]storageEntry

type storageEntry struct {
	Key   *common.Hash `json:"key"`
	Value common.Hash  `json:"value"`
}

// StorageRangeAt returns the storage at the given block height and transaction index.
func (api *DebugAPI) StorageRangeAt(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash, txIndex int, contractAddress common.Address, keyStart hexutil.Bytes, maxResult int) (StorageRangeResult, error) {
	if api.isFirewood() {
		return StorageRangeResult{}, errFirewoodNotSupported
	}

	block, err := api.eth.APIBackend.BlockByNumberOrHash(ctx, blockNrOrHash)
	if err != nil {
		return StorageRangeResult{}, err
	}
	if block == nil {
		return StorageRangeResult{}, fmt.Errorf("block %v not found", blockNrOrHash)
	}
	_, _, statedb, release, err := api.eth.stateAtTransaction(ctx, block, txIndex, 0)
	if err != nil {
		return StorageRangeResult{}, err
	}
	defer release()

	return storageRangeAt(statedb, block.Root(), contractAddress, keyStart, maxResult)
}

func storageRangeAt(statedb *state.StateDB, root common.Hash, address common.Address, start []byte, maxResult int) (StorageRangeResult, error) {
	storageRoot := statedb.GetStorageRoot(address)
	if storageRoot == types.EmptyRootHash || storageRoot == (common.Hash{}) {
		return StorageRangeResult{}, nil // empty storage
	}
	id := trie.StorageTrieID(root, crypto.Keccak256Hash(address.Bytes()), storageRoot)
	tr, err := trie.NewStateTrie(id, statedb.Database().TrieDB())
	if err != nil {
		return StorageRangeResult{}, err
	}
	trieIt, err := tr.NodeIterator(start)
	if err != nil {
		return StorageRangeResult{}, err
	}
	it := trie.NewIterator(trieIt)
	result := StorageRangeResult{Storage: storageMap{}}
	for i := 0; i < maxResult && it.Next(); i++ {
		_, content, _, err := rlp.Split(it.Value)
		if err != nil {
			return StorageRangeResult{}, err
		}
		e := storageEntry{Value: common.BytesToHash(content)}
		if preimage := tr.GetKey(it.Key); preimage != nil {
			preimage := common.BytesToHash(preimage)
			e.Key = &preimage
		}
		result.Storage[common.BytesToHash(it.Key)] = e
	}
	// Add the 'next key' so clients can continue downloading.
	if it.Next() {
		next := common.BytesToHash(it.Key)
		result.NextKey = &next
	}
	return result, nil
}

// GetModifiedAccountsByNumber returns all accounts that have changed between the
// two blocks specified. A change is defined as a difference in nonce, balance,
// code hash, or storage hash.
//
// With one parameter, returns the list of accounts modified in the specified block.
func (api *DebugAPI) GetModifiedAccountsByNumber(startNum uint64, endNum *uint64) ([]common.Address, error) {
	if api.isFirewood() {
		return nil, errFirewoodNotSupported
	}

	var startBlock, endBlock *types.Block
	startBlock = api.eth.blockchain.GetBlockByNumber(startNum)
	if startBlock == nil {
		return nil, fmt.Errorf("start block %x not found", startNum)
	}

	if endNum == nil {
		endBlock = startBlock
		startBlock = api.eth.blockchain.GetBlockByHash(startBlock.ParentHash())
		if startBlock == nil {
			return nil, fmt.Errorf("block %x has no parent", endBlock.Number())
		}
	} else {
		endBlock = api.eth.blockchain.GetBlockByNumber(*endNum)
		if endBlock == nil {
			return nil, fmt.Errorf("end block %d not found", *endNum)
		}
	}
	return api.getModifiedAccounts(startBlock, endBlock)
}

// GetModifiedAccountsByHash returns all accounts that have changed between the
// two blocks specified. A change is defined as a difference in nonce, balance,
// code hash, or storage hash.
//
// With one parameter, returns the list of accounts modified in the specified block.
func (api *DebugAPI) GetModifiedAccountsByHash(startHash common.Hash, endHash *common.Hash) ([]common.Address, error) {
	if api.isFirewood() {
		return nil, errFirewoodNotSupported
	}

	var startBlock, endBlock *types.Block
	startBlock = api.eth.blockchain.GetBlockByHash(startHash)
	if startBlock == nil {
		return nil, fmt.Errorf("start block %x not found", startHash)
	}

	if endHash == nil {
		endBlock = startBlock
		startBlock = api.eth.blockchain.GetBlockByHash(startBlock.ParentHash())
		if startBlock == nil {
			return nil, fmt.Errorf("block %x has no parent", endBlock.Number())
		}
	} else {
		endBlock = api.eth.blockchain.GetBlockByHash(*endHash)
		if endBlock == nil {
			return nil, fmt.Errorf("end block %x not found", *endHash)
		}
	}
	return api.getModifiedAccounts(startBlock, endBlock)
}

func (api *DebugAPI) getModifiedAccounts(startBlock, endBlock *types.Block) ([]common.Address, error) {
	if startBlock.Number().Uint64() >= endBlock.Number().Uint64() {
		return nil, fmt.Errorf("start block height (%d) must be less than end block height (%d)", startBlock.Number().Uint64(), endBlock.Number().Uint64())
	}
	triedb := api.eth.BlockChain().TrieDB()

	oldTrie, err := trie.NewStateTrie(trie.StateTrieID(startBlock.Root()), triedb)
	if err != nil {
		return nil, err
	}
	newTrie, err := trie.NewStateTrie(trie.StateTrieID(endBlock.Root()), triedb)
	if err != nil {
		return nil, err
	}
	oldIt, err := oldTrie.NodeIterator([]byte{})
	if err != nil {
		return nil, err
	}
	newIt, err := newTrie.NodeIterator([]byte{})
	if err != nil {
		return nil, err
	}
	diff, _ := trie.NewDifferenceIterator(oldIt, newIt)
	iter := trie.NewIterator(diff)

	var dirty []common.Address
	for iter.Next() {
		key := newTrie.GetKey(iter.Key)
		if key == nil {
			return nil, fmt.Errorf("no preimage found for hash %x", iter.Key)
		}
		dirty = append(dirty, common.BytesToAddress(key))
	}
	return dirty, nil
}

// GetAccessibleState returns the first number where the node has accessible
// state on disk. Note this being the post-state of that block and the pre-state
// of the next block.
// The (from, to) parameters are the sequence of blocks to search, which can go
// either forwards or backwards
func (api *DebugAPI) GetAccessibleState(from, to rpc.BlockNumber) (uint64, error) {
	if api.eth.blockchain.TrieDB().Scheme() == rawdb.PathScheme {
		return 0, errors.New("state history is not yet available in path-based scheme")
	}
	var resolveNum = func(num rpc.BlockNumber) (uint64, error) {
		// We don't have state for pending (-2), so treat it as latest
		if num.Int64() < 0 {
			block := api.eth.blockchain.CurrentBlock()
			if block == nil {
				return 0, errors.New("current block missing")
			}
			return block.Number.Uint64(), nil
		}
		return uint64(num.Int64()), nil
	}
	var (
		start   uint64
		end     uint64
		delta   = int64(1)
		lastLog time.Time
		err     error
	)
	if start, err = resolveNum(from); err != nil {
		return 0, err
	}
	if end, err = resolveNum(to); err != nil {
		return 0, err
	}
	if start == end {
		return 0, errors.New("from and to needs to be different")
	}
	if start > end {
		delta = -1
	}
	for i := int64(start); i != int64(end); i += delta {
		if time.Since(lastLog) > 8*time.Second {
			log.Info("Finding roots", "from", start, "to", end, "at", i)
			lastLog = time.Now()
		}
		h := api.eth.BlockChain().GetHeaderByNumber(uint64(i))
		if h == nil {
			return 0, fmt.Errorf("missing header %d", i)
		}
		if api.eth.BlockChain().HasState(h.Root) {
			return uint64(i), nil
		}
	}
	return 0, errors.New("no state found")
}

func (api *DebugAPI) isFirewood() bool {
	return api.eth.blockchain.CacheConfig().StateScheme == customrawdb.FirewoodScheme
}
