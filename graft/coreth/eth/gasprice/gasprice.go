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
// Copyright 2015 The go-ethereum Authors
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

package gasprice

import (
	"context"
	"math/big"
	"sort"
	"sync"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var DefaultMaxPrice = big.NewInt(500 * params.GWei)

type Config struct {
	Blocks     int
	Percentile int
	Default    *big.Int `toml:",omitempty"`
	MaxPrice   *big.Int `toml:",omitempty"`
}

// OracleBackend includes all necessary background APIs for oracle.
type OracleBackend interface {
	HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error)
	BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error)
	ChainConfig() *params.ChainConfig
}

// Oracle recommends gas prices based on the content of recent
// blocks. Suitable for both light and full clients.
type Oracle struct {
	backend   OracleBackend
	lastHead  common.Hash
	lastPrice *big.Int
	maxPrice  *big.Int
	cacheLock sync.RWMutex
	fetchLock sync.Mutex

	checkBlocks int
	percentile  int
}

// NewOracle returns a new gasprice oracle which can recommend suitable
// gasprice for newly created transaction.
func NewOracle(backend OracleBackend, params Config) *Oracle {
	blocks := params.Blocks
	if blocks < 1 {
		blocks = 1
		log.Warn("Sanitizing invalid gasprice oracle sample blocks", "provided", params.Blocks, "updated", blocks)
	}
	percent := params.Percentile
	if percent < 0 {
		percent = 0
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", params.Percentile, "updated", percent)
	}
	if percent > 100 {
		percent = 100
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", params.Percentile, "updated", percent)
	}
	maxPrice := params.MaxPrice
	if maxPrice == nil || maxPrice.Int64() <= 0 {
		maxPrice = DefaultMaxPrice
		log.Warn("Sanitizing invalid gasprice oracle price cap", "provided", params.MaxPrice, "updated", maxPrice)
	}
	return &Oracle{
		backend:     backend,
		lastPrice:   params.Default,
		maxPrice:    maxPrice,
		checkBlocks: blocks,
		percentile:  percent,
	}
}

// SuggestPrice returns a gasprice so that newly created transaction can
// have a very high chance to be included in the following blocks.
func (gpo *Oracle) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return params.MinGasPrice, nil
}

type getBlockPricesResult struct {
	prices []*big.Int
	err    error
}

type transactionsByGasPrice []*types.Transaction

func (t transactionsByGasPrice) Len() int           { return len(t) }
func (t transactionsByGasPrice) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }
func (t transactionsByGasPrice) Less(i, j int) bool { return t[i].GasPriceCmp(t[j]) < 0 }

// getBlockPrices calculates the lowest transaction gas price in a given block
// and sends it to the result channel. If the block is empty or all transactions
// are sent by the miner itself(it doesn't make any sense to include this kind of
// transaction prices for sampling), nil gasprice is returned.
func (gpo *Oracle) getBlockPrices(ctx context.Context, signer types.Signer, blockNum uint64, limit int, result chan getBlockPricesResult, quit chan struct{}) {
	block, err := gpo.backend.BlockByNumber(ctx, rpc.BlockNumber(blockNum))
	if block == nil {
		select {
		case result <- getBlockPricesResult{nil, err}:
		case <-quit:
		}
		return
	}
	blockTxs := block.Transactions()
	txs := make([]*types.Transaction, len(blockTxs))
	copy(txs, blockTxs)
	sort.Sort(transactionsByGasPrice(txs))

	var prices []*big.Int
	for _, tx := range txs {
		sender, err := types.Sender(signer, tx)
		if err == nil && sender != block.Coinbase() {
			prices = append(prices, tx.GasPrice())
			if len(prices) >= limit {
				break
			}
		}
	}
	select {
	case result <- getBlockPricesResult{prices, nil}:
	case <-quit:
	}
}

type bigIntArray []*big.Int

func (s bigIntArray) Len() int           { return len(s) }
func (s bigIntArray) Less(i, j int) bool { return s[i].Cmp(s[j]) < 0 }
func (s bigIntArray) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
