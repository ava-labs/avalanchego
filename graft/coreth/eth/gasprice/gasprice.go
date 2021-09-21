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

	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	DefaultMaxPrice   = big.NewInt(150 * params.GWei)
	DefaultMinPrice   = big.NewInt(0 * params.GWei)
	DefaultMinGasUsed = big.NewInt(2_000_000) // block gas limit is 8,000,000
)

type Config struct {
	Blocks     int
	Percentile int
	MaxPrice   *big.Int `toml:",omitempty"`
	MinPrice   *big.Int `toml:",omitempty"`
	MinGasUsed *big.Int `toml:",omitempty"`
}

// OracleBackend includes all necessary background APIs for oracle.
type OracleBackend interface {
	HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error)
	BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error)
	ChainConfig() *params.ChainConfig
	MinRequiredTip(ctx context.Context, header *types.Header) (*big.Int, error)
}

// Oracle recommends gas prices based on the content of recent
// blocks. Suitable for both light and full clients.
type Oracle struct {
	backend   OracleBackend
	lastHead  common.Hash
	lastPrice *big.Int
	// [minPrice] ensures we don't get into a positive feedback loop where tips
	// sink to 0 during a period of slow block production, such that nobody's
	// transactions will be included until the full block fee duration has
	// elapsed.
	minPrice *big.Int
	maxPrice *big.Int
	// [minGasUsed] ensures we don't recommend users pay non-zero tips when other
	// users are paying a tip to unnecessarily expedite block production.
	minGasUsed *big.Int
	cacheLock  sync.RWMutex
	fetchLock  sync.Mutex

	// clock to decide what set of rules to use when recommending a gas price
	clock timer.Clock

	checkBlocks, percentile int
}

// NewOracle returns a new gasprice oracle which can recommend suitable
// gasprice for newly created transaction.
func NewOracle(backend OracleBackend, config Config) *Oracle {
	blocks := config.Blocks
	if blocks < 1 {
		blocks = 1
		log.Warn("Sanitizing invalid gasprice oracle sample blocks", "provided", config.Blocks, "updated", blocks)
	}
	percent := config.Percentile
	if percent < 0 {
		percent = 0
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", config.Percentile, "updated", percent)
	}
	if percent > 100 {
		percent = 100
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", config.Percentile, "updated", percent)
	}
	maxPrice := config.MaxPrice
	if maxPrice == nil || maxPrice.Int64() <= 0 {
		maxPrice = DefaultMaxPrice
		log.Warn("Sanitizing invalid gasprice oracle max price", "provided", config.MaxPrice, "updated", maxPrice)
	}
	minPrice := config.MinPrice
	if minPrice == nil || minPrice.Int64() < 0 {
		minPrice = DefaultMinPrice
		log.Warn("Sanitizing invalid gasprice oracle min price", "provided", config.MinPrice, "updated", minPrice)
	}
	minGasUsed := config.MinGasUsed
	if minGasUsed == nil || minGasUsed.Int64() < 0 {
		minGasUsed = DefaultMinGasUsed
		log.Warn("Sanitizing invalid gasprice oracle min gas used", "provided", config.MinGasUsed, "updated", minGasUsed)
	}
	return &Oracle{
		backend:     backend,
		lastPrice:   minPrice,
		minPrice:    minPrice,
		maxPrice:    maxPrice,
		minGasUsed:  minGasUsed,
		checkBlocks: blocks,
		percentile:  percent,
	}
}

// EstiamteBaseFee returns an estimate of what the base fee will be on a block
// produced at the current time. If ApricotPhase3 has not been activated, it may
// return a nil value and a nil error.
func (oracle *Oracle) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	// Fetch the most recent block by number
	block, err := oracle.backend.BlockByNumber(ctx, rpc.LatestBlockNumber)
	if err != nil {
		return nil, err
	}
	// If the fetched block does not have a base fee, return nil as the base fee
	if block.BaseFee() == nil {
		return nil, nil
	}

	// If the block does have a baseFee, calculate the next base fee
	// based on the current time and add it to the tip to estimate the
	// total gas price estimate.
	_, nextBaseFee, err := dummy.CalcBaseFee(oracle.backend.ChainConfig(), block.Header(), oracle.clock.Unix())
	return nextBaseFee, err
}

// SuggestPrice returns an estimated price for legacy transactions.
func (oracle *Oracle) SuggestPrice(ctx context.Context) (*big.Int, error) {
	// Estimate the effective tip based on recent blocks.
	tip, err := oracle.suggestTipCap(ctx)
	if err != nil {
		return nil, err
	}
	nextBaseFee, err := oracle.EstimateBaseFee(ctx)
	if err != nil {
		return nil, err
	}
	// If [nextBaseFee] is nil, return [tip] without modification.
	if nextBaseFee == nil {
		return tip, nil
	}

	return new(big.Int).Add(tip, nextBaseFee), nil
}

// SuggestTipCap returns a tip cap so that newly created transaction can have a
// very high chance to be included in the following blocks.
//
// Note, for legacy transactions and the legacy eth_gasPrice RPC call, it will be
// necessary to add the basefee to the returned number to fall back to the legacy
// behavior.
func (oracle *Oracle) SuggestTipCap(ctx context.Context) (*big.Int, error) {
	return oracle.suggestTipCap(ctx)
}

// sugggestTipCap checks the clock to estimate what network rules will be applied to
// new transactions and then suggests a gas tip cap based on the response.
func (oracle *Oracle) suggestTipCap(ctx context.Context) (*big.Int, error) {
	bigTimestamp := big.NewInt(oracle.clock.Time().Unix())

	switch {
	case oracle.backend.ChainConfig().IsApricotPhase4(bigTimestamp):
		return oracle.suggestDynamicTipCap(ctx)
	case oracle.backend.ChainConfig().IsApricotPhase3(bigTimestamp):
		return new(big.Int).Set(common.Big0), nil
	case oracle.backend.ChainConfig().IsApricotPhase1(bigTimestamp):
		return big.NewInt(params.ApricotPhase1MinGasPrice), nil
	default:
		return big.NewInt(params.LaunchMinGasPrice), nil
	}
}

// suggestDynamicTipCap estimates the gas tip based on a simple sampling method
func (oracle *Oracle) suggestDynamicTipCap(ctx context.Context) (*big.Int, error) {
	head, _ := oracle.backend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
	headHash := head.Hash()

	// If the latest gasprice is still available, return it.
	oracle.cacheLock.RLock()
	lastHead, lastPrice := oracle.lastHead, oracle.lastPrice
	oracle.cacheLock.RUnlock()
	if headHash == lastHead {
		return new(big.Int).Set(lastPrice), nil
	}
	oracle.fetchLock.Lock()
	defer oracle.fetchLock.Unlock()

	// Try checking the cache again, maybe the last fetch fetched what we need
	oracle.cacheLock.RLock()
	lastHead, lastPrice = oracle.lastHead, oracle.lastPrice
	oracle.cacheLock.RUnlock()
	if headHash == lastHead {
		return new(big.Int).Set(lastPrice), nil
	}
	var (
		sent, exp int
		number    = head.Number.Uint64()
		result    = make(chan results, oracle.checkBlocks)
		quit      = make(chan struct{})
		results   []*big.Int
	)
	for sent < oracle.checkBlocks && number > 0 {
		go oracle.getBlockTips(ctx, number, result, quit)
		sent++
		exp++
		number--
	}
	for exp > 0 {
		res := <-result
		if res.err != nil {
			close(quit)
			return new(big.Int).Set(lastPrice), res.err
		}
		exp--
		if res.value != nil {
			results = append(results, res.value)
		} else {
			results = append(results, new(big.Int).Set(common.Big0))
		}
	}
	price := lastPrice
	if len(results) > 0 {
		sort.Sort(bigIntArray(results))
		price = results[(len(results)-1)*oracle.percentile/100]
	}
	if price.Cmp(oracle.maxPrice) > 0 {
		price = new(big.Int).Set(oracle.maxPrice)
	}
	if price.Cmp(oracle.minPrice) < 0 {
		price = new(big.Int).Set(oracle.minPrice)
	}
	oracle.cacheLock.Lock()
	oracle.lastHead = headHash
	oracle.lastPrice = price
	oracle.cacheLock.Unlock()

	return new(big.Int).Set(price), nil
}

type results struct {
	value *big.Int
	err   error
}

// getBlockTips calculates the minimum required tip to be included in a given
// block and sends the value to the result channel.
func (oracle *Oracle) getBlockTips(ctx context.Context, blockNum uint64, result chan results, quit chan struct{}) {
	header, err := oracle.backend.HeaderByNumber(ctx, rpc.BlockNumber(blockNum))
	if header == nil {
		select {
		case result <- results{nil, err}:
		case <-quit:
		}
		return
	}

	// Don't bias the estimate with blocks containing a limited number of transactions paying to
	// expedite block production.
	if header.GasUsed < oracle.minGasUsed.Uint64() {
		select {
		case result <- results{nil, nil}:
		case <-quit:
		}
		return
	}

	// Compute minimum required tip to be included in previous block
	//
	// NOTE: Using this approach, we will never recommend that the caller
	// provides a non-zero tip unless some block is produced faster than the
	// target rate (which could only occur if some set of callers manually override the
	// suggested tip). In the future, we may wish to start suggesting a non-zero
	// tip when most blocks are full otherwise callers may observe an unexpected
	// delay in transaction inclusion.
	minTip, err := oracle.backend.MinRequiredTip(ctx, header)
	select {
	case result <- results{minTip, err}:
	case <-quit:
	}
}

type bigIntArray []*big.Int

func (s bigIntArray) Len() int           { return len(s) }
func (s bigIntArray) Less(i, j int) bool { return s[i].Cmp(s[j]) < 0 }
func (s bigIntArray) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
