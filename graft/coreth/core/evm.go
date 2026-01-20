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
// Copyright 2016 The go-ethereum Authors
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

package core

import (
	"bytes"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus/misc/eip4844"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
)

// RegisterExtras registers hooks with libevm to achieve Avalanche behaviour of
// the EVM. It MUST NOT be called more than once and therefore is only allowed
// to be used in tests and `package main`, to avoid polluting other packages
// that transitively depend on this one but don't need registration.
func RegisterExtras() {
	// Although the registration function refers to just Hooks (not Extras) this
	// will be changed in the future to standardise across libevm, hence the
	// name of the function we're in.
	vm.RegisterHooks(hooks{})
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
//
// This function is not intended for direct use. Use
// `evm.WithTempRegisteredLibEVMExtras()` instead as it calls this along with
// all other temporary-registration functions.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	return vm.WithTempRegisteredHooks(lock, hooks{}, fn)
}

type hooks struct{}

// PreprocessingGasCharge is not necessary.
func (hooks) PreprocessingGasCharge(common.Hash) (uint64, error) {
	return 0, nil
}

// OverrideNewEVMArgs is a hook that is called in [vm.NewEVM].
// It allows for the modification of the EVM arguments before the EVM is created.
// Specifically, we set Random to be the same as Difficulty since Shanghai.
// This allows using the same jump table as upstream.
// Then we set Difficulty to 0 as it is post Merge in upstream.
// Additionally we wrap the StateDB with the appropriate StateDB wrapper,
// which is used in coreth to process historical pre-AP1 blocks with the
// [StateDbAP1.GetCommittedState] method as it was historically.
func (hooks) OverrideNewEVMArgs(args *vm.NewEVMArgs) *vm.NewEVMArgs {
	rules := args.ChainConfig.Rules(args.BlockContext.BlockNumber, params.IsMergeTODO, args.BlockContext.Time)
	args.StateDB = wrapStateDB(rules, args.StateDB)

	if rules.IsShanghai {
		args.BlockContext.Random = new(common.Hash)
		args.BlockContext.Random.SetBytes(args.BlockContext.Difficulty.Bytes())
		args.BlockContext.Difficulty = new(big.Int)
	}

	return args
}

func (hooks) OverrideEVMResetArgs(rules params.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(rules, args.StateDB)
	return args
}

func wrapStateDB(rules params.Rules, statedb vm.StateDB) vm.StateDB {
	wrappedStateDB := extstate.New(statedb.(*state.StateDB))
	if params.GetRulesExtra(rules).IsApricotPhase1 {
		return wrappedStateDB
	}
	return &StateDBAP0{wrappedStateDB}
}

// StateDBAP0 implements the GetCommittedState behavior that existed prior to
// the AP1 upgrade.
//
// Since launch, state keys have been normalized to allow for multicoin
// balances. However, at launch GetCommittedState was not updated. This meant
// that gas refunds were not calculated as expected for SSTORE opcodes.
//
// This oversight was fixed in AP1, but in order to execute blocks prior to AP1
// and generate the same merkle root, this behavior must be maintained.
//
// See the [extstate] package for details around state key normalization.
type StateDBAP0 struct {
	*extstate.StateDB
}

func (s *StateDBAP0) GetCommittedState(addr common.Address, key common.Hash, _ ...stateconf.StateDBStateOption) common.Hash {
	return s.StateDB.GetCommittedState(addr, key, stateconf.SkipStateKeyTransformation())
}

// ChainContext supports retrieving headers and consensus parameters from the
// current blockchain to be used during transaction processing.
type ChainContext interface {
	// Engine retrieves the chain's consensus engine.
	Engine() consensus.Engine

	// GetHeader returns the header corresponding to the hash/number argument pair.
	GetHeader(common.Hash, uint64) *types.Header
}

// NewEVMBlockContext creates a new context for use in the EVM.
func NewEVMBlockContext(header *types.Header, chain ChainContext, author *common.Address) vm.BlockContext {
	var (
		beneficiary common.Address
		baseFee     *big.Int
		blobBaseFee *big.Int
	)

	// If we don't have an explicit author (i.e. not mining), extract from the header
	if author == nil {
		beneficiary, _ = chain.Engine().Author(header) // Ignore error, we're past header validation
	} else {
		beneficiary = *author
	}
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.ExcessBlobGas != nil {
		blobBaseFee = eip4844.CalcBlobFee(*header.ExcessBlobGas)
	}
	return vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     GetHashFn(header, chain),
		Coinbase:    beneficiary,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        header.Time,
		Difficulty:  new(big.Int).Set(header.Difficulty),
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
		GasLimit:    header.GasLimit,
		Header: &types.Header{
			Number: new(big.Int).Set(header.Number),
			Time:   header.Time,
			Extra:  header.Extra,
		},
	}
}

// NewEVMBlockContextWithPredicateResults creates a new context for use in the
// EVM with an override for the predicate results. The miner uses this to pass
// predicate results to the EVM when header.Extra is not fully formed yet.
func NewEVMBlockContextWithPredicateResults(rules extras.AvalancheRules, header *types.Header, chain ChainContext, author *common.Address, predicateBytes []byte) vm.BlockContext {
	blockCtx := NewEVMBlockContext(header, chain, author)
	// Note this only sets the block context, which is the hand-off point for
	// the EVM. The actual header is not modified.
	blockCtx.Header.Extra = customheader.SetPredicateBytesInExtra(
		rules,
		bytes.Clone(header.Extra),
		predicateBytes,
	)
	return blockCtx
}

// NewEVMTxContext creates a new transaction context for a single transaction.
func NewEVMTxContext(msg *Message) vm.TxContext {
	ctx := vm.TxContext{
		Origin:     msg.From,
		GasPrice:   new(big.Int).Set(msg.GasPrice),
		BlobHashes: msg.BlobHashes,
	}
	if msg.BlobGasFeeCap != nil {
		ctx.BlobFeeCap = new(big.Int).Set(msg.BlobGasFeeCap)
	}
	return ctx
}

// GetHashFn returns a GetHashFunc which retrieves header hashes by number
func GetHashFn(ref *types.Header, chain ChainContext) func(n uint64) common.Hash {
	// Cache will initially contain [refHash.parent],
	// Then fill up with [refHash.p, refHash.pp, refHash.ppp, ...]
	var cache []common.Hash

	return func(n uint64) common.Hash {
		if ref.Number.Uint64() <= n {
			// This situation can happen if we're doing tracing and using
			// block overrides.
			return common.Hash{}
		}
		// If there's no hash cache yet, make one
		if len(cache) == 0 {
			cache = append(cache, ref.ParentHash)
		}
		if idx := ref.Number.Uint64() - n - 1; idx < uint64(len(cache)) {
			return cache[idx]
		}
		// No luck in the cache, but we can start iterating from the last element we already know
		lastKnownHash := cache[len(cache)-1]
		lastKnownNumber := ref.Number.Uint64() - uint64(len(cache))

		for {
			header := chain.GetHeader(lastKnownHash, lastKnownNumber)
			if header == nil {
				break
			}
			cache = append(cache, header.ParentHash)
			lastKnownHash = header.ParentHash
			lastKnownNumber = header.Number.Uint64() - 1
			if n == lastKnownNumber {
				return lastKnownHash
			}
		}
		return common.Hash{}
	}
}

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
