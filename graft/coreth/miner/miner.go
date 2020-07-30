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
	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/common/hexutil"
	"github.com/ava-labs/go-ethereum/event"
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
	ManualUncle  bool
	DisableUncle bool
}

type Miner struct {
	w *worker
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(block *types.Block) bool, mcb *MinerCallbacks) *Miner {
	return &Miner{
		w: newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, mcb),
	}
}

func (self *Miner) Start(coinbase common.Address) {
	self.w.start()
}

func (self *Miner) Stop() {
	self.w.stop()
}

func (self *Miner) Mining() bool {
	return false
}

func (self *Miner) HashRate() uint64 {
	return 0
}

func (self *Miner) SetExtra(extra []byte) error {
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("Extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
	}
	self.w.setExtra(extra)
	return nil
}

func (self *Miner) SetRecommitInterval(interval time.Duration) {
	self.w.setRecommitInterval(interval)
}

func (self *Miner) Pending() (*types.Block, *state.StateDB) {
	return self.w.pending()
}

func (self *Miner) PendingBlock() *types.Block {
	return self.w.pendingBlock()
}

func (self *Miner) SetEtherbase(addr common.Address) {
	self.w.setEtherbase(addr)
}

func (self *Miner) GenBlock() {
	self.w.genBlock()
}
