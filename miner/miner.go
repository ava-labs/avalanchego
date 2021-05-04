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
	"math/big"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// Backend wraps all methods required for mining.
type Backend interface {
	BlockChain() *core.BlockChain
	TxPool() *core.TxPool
}

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase common.Address `toml:",omitempty"` // Public address for block mining rewards (default = first account)
	Notify    []string       `toml:",omitempty"` // HTTP URL list to be notified of new work packages(only useful in ethash).
	// ExtraData    hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	GasFloor              uint64        // Target gas floor for mined blocks.
	GasCeil               uint64        // Target gas ceiling for mined blocks.
	ApricotPhase1GasLimit uint64        // Gas Limit for mined blocks as of Apricot Phase 1.
	GasPrice              *big.Int      // Minimum gas price for mining a transaction
	Recommit              time.Duration // The time interval for miner to re-create mining work.
	Noverify              bool          // Disable remote mining solution verification(only useful in ethash).
}

type Miner struct {
	// Original code:
	// mux      *event.TypeMux
	// coinbase common.Address
	// eth      Backend
	// engine   consensus.Engine
	// exitCh   chan struct{}
	// startCh  chan common.Address
	// stopCh   chan struct{}
	worker *worker
}

func New(eth Backend, config *Config, chainConfig *params.ChainConfig, mux *event.TypeMux, engine consensus.Engine, isLocalBlock func(block *types.Block) bool, mcb *MinerCallbacks) *Miner {
	return &Miner{
		// Original code:
		// eth:     eth,
		// mux:     mux,
		// engine:  engine,
		// exitCh:  make(chan struct{}),
		// startCh: make(chan common.Address),
		// stopCh:  make(chan struct{}),
		worker: newWorker(config, chainConfig, engine, eth, mux, isLocalBlock, true, mcb),
	}
}

// Original code:
// // update keeps track of the downloader events. Please be aware that this is a one shot type of update loop.
// // It's entered once and as soon as `Done` or `Failed` has been broadcasted the events are unregistered and
// // the loop is exited. This to prevent a major security vuln where external parties can DOS you with blocks
// // and halt your mining operation for as long as the DOS continues.
// func (miner *Miner) update() {
// 	events := miner.mux.Subscribe(downloader.StartEvent{}, downloader.DoneEvent{}, downloader.FailedEvent{})
// 	defer func() {
// 		if !events.Closed() {
// 			events.Unsubscribe()
// 		}
// 	}()
//
// 	shouldStart := false
// 	canStart := true
// 	dlEventCh := events.Chan()
// 	for {
// 		select {
// 		case ev := <-dlEventCh:
// 			if ev == nil {
// 				// Unsubscription done, stop listening
// 				dlEventCh = nil
// 				continue
// 			}
// 			switch ev.Data.(type) {
// 			case downloader.StartEvent:
// 				wasMining := miner.Mining()
// 				miner.worker.stop()
// 				canStart = false
// 				if wasMining {
// 					// Resume mining after sync was finished
// 					shouldStart = true
// 					log.Info("Mining aborted due to sync")
// 				}
// 			case downloader.FailedEvent:
// 				canStart = true
// 				if shouldStart {
// 					miner.SetEtherbase(miner.coinbase)
// 					miner.worker.start()
// 				}
// 			case downloader.DoneEvent:
// 				canStart = true
// 				if shouldStart {
// 					miner.SetEtherbase(miner.coinbase)
// 					miner.worker.start()
// 				}
// 				// Stop reacting to downloader events
// 				events.Unsubscribe()
// 			}
// 		case addr := <-miner.startCh:
// 			miner.SetEtherbase(addr)
// 			if canStart {
// 				miner.worker.start()
// 			}
// 			shouldStart = true
// 		case <-miner.stopCh:
// 			shouldStart = false
// 			miner.worker.stop()
// 		case <-miner.exitCh:
// 			miner.worker.close()
// 			return
// 		}
// 	}
// }

func (miner *Miner) Start(coinbase common.Address) {
	miner.worker.start()
}

func (miner *Miner) Stop() {
	miner.worker.stop()
}

func (miner *Miner) Mining() bool {
	return miner.worker.isRunning()
}

func (miner *Miner) HashRate() uint64 {
	// Original code:
	// if pow, ok := miner.engine.(consensus.PoW); ok {
	// 	return uint64(pow.Hashrate())
	// }
	return 0
}

// Original Code:
// func (miner *Miner) SetExtra(extra []byte) error {
// 	if uint64(len(extra)) > params.MaximumExtraDataSize {
// 		return fmt.Errorf("extra exceeds max length. %d > %v", len(extra), params.MaximumExtraDataSize)
// 	}
// 	miner.worker.setExtra(extra)
// 	return nil
// }

// SetRecommitInterval sets the interval for sealing work resubmitting.
func (miner *Miner) SetRecommitInterval(interval time.Duration) {
	miner.worker.setRecommitInterval(interval)
}

// Original code:
// Pending returns the currently pending block and associated state.
// func (miner *Miner) Pending() (*types.Block, *state.StateDB) {
// 	return miner.worker.pending()
// }
//
// PendingBlock returns the currently pending block.
//
// Note, to access both the pending block and the pending state
// simultaneously, please use Pending(), as the pending state can
// change between multiple method calls
// func (miner *Miner) PendingBlock() *types.Block {
// 	return miner.worker.pendingBlock()
// }

func (miner *Miner) SetEtherbase(addr common.Address) {
	// Original code:
	// miner.coinbase = addr
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
