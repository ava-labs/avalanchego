// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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

// Package miner implements Ethereum block creation and mining.
package miner

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/event"
)

// Backend wraps all methods required for mining.
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *core.TxPool
}

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase    common.Address `toml:",omitempty"` // Public address for block mining rewards (default = first account)
	Notify       []string       `toml:",omitempty"` // HTTP URL list to be notified of new work packages(only useful in ethash).
	ExtraData    hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	GasFloor     uint64         // Target gas floor for mined blocks.
	GasCeil      uint64         // Target gas ceiling for mined blocks.
	GasPrice     *big.Int       // Minimum gas price for mining a transaction
	Recommit     time.Duration  // The time interval for miner to re-create mining work.
	Noverify     bool           // Disable remote mining solution verification(only useful in ethash).
	ManualMining bool
}

type Miner struct {
	worker *worker
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(block *types.Block) bool, mcb *MinerCallbacks) *Miner {
	return &Miner{
		worker: newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, true, mcb),
	}
}

func (miner *Miner) Start(coinbase common.Address) {
	miner.worker.start()
}

func (miner *Miner) Stop() {
	miner.worker.stop()
}

func (miner *Miner) Mining() bool {
	return false
}

func (miner *Miner) HashRate() uint64 {
	return 0
}

func (miner *Miner) SetExtra(extra []byte) error {
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
	}
	miner.worker.setExtra(extra)
	return nil
}

// SetRecommitInterval sets the interval for sealing work resubmitting.
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// Pending returns the currently pending block and associated state.
func (miner *Miner) Pending() (*types.Block, *state.StateDB) {
	return miner.worker.pending()
}

// PendingBlock returns the currently pending block.
//
// Note, to access both the pending block and the pending state
// simultaneously, please use Pending(), as the pending state can
// change between multiple method calls
func (miner *Miner) PendingBlock() *types.Block {
	return miner.worker.pendingBlock()
}

func (miner *Miner) SetEtherbase(addr common.Address) {
	miner.worker.setEtherbase(addr)
}

// EnablePreseal turns on the preseal mining feature. It's enabled by default.
// Note this function shouldn't be exposed to API, it's unnecessary for users
// (miners) to actually know the underlying detail. It's only for outside project
// which uses this library.
func (miner *Miner) EnablePreseal() {
	miner.worker.enablePreseal()
}

// DisablePreseal turns off the preseal mining feature. It's necessary for some
// fake consensus engine which can seal blocks instantaneously.
// Note this function shouldn't be exposed to API, it's unnecessary for users
// (miners) to actually know the underlying detail. It's only for outside project
// which uses this library.
func (miner *Miner) DisablePreseal() {
	miner.worker.disablePreseal()
}

// SubscribePendingLogs starts delivering logs from pending transactions
// to the given channel.
func (miner *Miner) SubscribePendingLogs(ch chan<- []*types.Log) event.Subscription {
	return miner.worker.pendingLogsFeed.Subscribe(ch)
}
func (miner *Miner) GenBlock() {
	miner.worker.genBlock()
}

func (miner *Miner) GetWorkerMux() *event.TypeMux {
	return miner.worker.mux
}
