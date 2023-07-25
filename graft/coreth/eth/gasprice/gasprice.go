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

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// DefaultMaxCallBlockHistory is the number of blocks that can be fetched in
	// a single call to eth_feeHistory.
	DefaultMaxCallBlockHistory = 2048
	// DefaultMaxBlockHistory is the number of blocks from the last accepted
	// block that can be fetched in eth_feeHistory.
	//
	// DefaultMaxBlockHistory is chosen to be a value larger than the required
	// fee lookback window that MetaMask uses (20k blocks).
	DefaultMaxBlockHistory = 25_000
	// DefaultFeeHistoryCacheSize is chosen to be some value larger than
	// [DefaultMaxBlockHistory] to ensure all block lookups can be cached when
	// serving a fee history query.
	DefaultFeeHistoryCacheSize = 30_000
)

var (
	DefaultMaxPrice           = big.NewInt(150 * params.GWei)
	DefaultMinPrice           = big.NewInt(0 * params.GWei)
	DefaultMinBaseFee         = big.NewInt(params.ApricotPhase3InitialBaseFee)
	DefaultMinGasUsed         = big.NewInt(6_000_000) // block gas limit is 8,000,000
	DefaultMaxLookbackSeconds = uint64(80)
)

type Config struct {
	// Blocks specifies the number of blocks to fetch during gas price estimation.
	Blocks int
	// Percentile is a value between 0 and 100 that we use during gas price estimation to choose
	// the gas price estimate in which Percentile% of the gas estimate values in the array fall below it
	Percentile int
	// MaxLookbackSeconds specifies the maximum number of seconds that current timestamp
	// can differ from block timestamp in order to be included in gas price estimation
	MaxLookbackSeconds uint64
	// MaxCallBlockHistory specifies the maximum number of blocks that can be
	// fetched in a single eth_feeHistory call.
	MaxCallBlockHistory uint64
	// MaxBlockHistory specifies the furthest back behind the last accepted block that can
	// be requested by fee history.
	MaxBlockHistory uint64
	MaxPrice        *big.Int `toml:",omitempty"`
	MinPrice        *big.Int `toml:",omitempty"`
	MinGasUsed      *big.Int `toml:",omitempty"`
}

// OracleBackend includes all necessary background APIs for oracle.
type OracleBackend interface {
	HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error)
	BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error)
	GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error)
	ChainConfig() *params.ChainConfig
	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
	SubscribeChainAcceptedEvent(ch chan<- core.ChainEvent) event.Subscription
	MinRequiredTip(ctx context.Context, header *types.Header) (*big.Int, error)
	LastAcceptedBlock() *types.Block
}

// Oracle recommends gas prices based on the content of recent
// blocks. Suitable for both light and full clients.
type Oracle struct {
	backend     OracleBackend
	lastHead    common.Hash
	lastPrice   *big.Int
	lastBaseFee *big.Int
	// [minPrice] ensures we don't get into a positive feedback loop where tips
	// sink to 0 during a period of slow block production, such that nobody's
	// transactions will be included until the full block fee duration has
	// elapsed.
	minPrice  *big.Int
	maxPrice  *big.Int
	cacheLock sync.RWMutex
	fetchLock sync.Mutex

	// clock to decide what set of rules to use when recommending a gas price
	clock mockable.Clock

	checkBlocks, percentile int
	maxLookbackSeconds      uint64
	maxCallBlockHistory     uint64
	maxBlockHistory         uint64
	historyCache            *lru.Cache[uint64, *slimBlock]
	feeInfoProvider         *feeInfoProvider
}

// NewOracle returns a new gasprice oracle which can recommend suitable
// gasprice for newly created transaction.
func NewOracle(backend OracleBackend, config Config) (*Oracle, error) {
	blocks := config.Blocks
	if blocks < 1 {
		blocks = 1
		log.Warn("Sanitizing invalid gasprice oracle sample blocks", "provided", config.Blocks, "updated", blocks)
	}
	percent := config.Percentile
	if percent < 0 {
		percent = 0
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", config.Percentile, "updated", percent)
	} else if percent > 100 {
		percent = 100
		log.Warn("Sanitizing invalid gasprice oracle sample percentile", "provided", config.Percentile, "updated", percent)
	}
	maxLookbackSeconds := config.MaxLookbackSeconds
	if maxLookbackSeconds <= 0 {
		maxLookbackSeconds = DefaultMaxLookbackSeconds
		log.Warn("Sanitizing invalid gasprice oracle max block seconds", "provided", config.MaxLookbackSeconds, "updated", maxLookbackSeconds)
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
	maxCallBlockHistory := config.MaxCallBlockHistory
	if maxCallBlockHistory < 1 {
		maxCallBlockHistory = DefaultMaxCallBlockHistory
		log.Warn("Sanitizing invalid gasprice oracle max call block history", "provided", config.MaxCallBlockHistory, "updated", maxCallBlockHistory)
	}
	maxBlockHistory := config.MaxBlockHistory
	if maxBlockHistory < 1 {
		maxBlockHistory = DefaultMaxBlockHistory
		log.Warn("Sanitizing invalid gasprice oracle max block history", "provided", config.MaxBlockHistory, "updated", maxBlockHistory)
	}

	cache := lru.NewCache[uint64, *slimBlock](DefaultFeeHistoryCacheSize)
	headEvent := make(chan core.ChainHeadEvent, 1)
	backend.SubscribeChainHeadEvent(headEvent)
	go func() {
		var lastHead common.Hash
		for ev := range headEvent {
			if ev.Block.ParentHash() != lastHead {
				cache.Purge()
			}
			lastHead = ev.Block.Hash()
		}
	}()
	feeInfoProvider, err := newFeeInfoProvider(backend, minGasUsed.Uint64(), config.Blocks)
	if err != nil {
		return nil, err
	}
	return &Oracle{
		backend:             backend,
		lastPrice:           minPrice,
		lastBaseFee:         DefaultMinBaseFee,
		minPrice:            minPrice,
		maxPrice:            maxPrice,
		checkBlocks:         blocks,
		percentile:          percent,
		maxLookbackSeconds:  maxLookbackSeconds,
		maxCallBlockHistory: maxCallBlockHistory,
		maxBlockHistory:     maxBlockHistory,
		historyCache:        cache,
		feeInfoProvider:     feeInfoProvider,
	}, nil
}

// EstimateBaseFee returns an estimate of what the base fee will be on a block
// produced at the current time. If ApricotPhase3 has not been activated, it may
// return a nil value and a nil error.
func (oracle *Oracle) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	_, baseFee, err := oracle.suggestDynamicFees(ctx)
	if err != nil {
		return nil, err
	}

	// We calculate the [nextBaseFee] if a block were to be produced immediately.
	// If [nextBaseFee] is lower than the estimate from sampling, then we return it
	// to prevent returning an incorrectly high fee when the network is quiescent.
	nextBaseFee, err := oracle.estimateNextBaseFee(ctx)
	if err != nil {
		log.Warn("failed to estimate next base fee", "err", err)
		return baseFee, nil
	}
	// If base fees have not been enabled, return a nil value.
	if nextBaseFee == nil {
		return nil, nil
	}

	baseFee = math.BigMin(baseFee, nextBaseFee)
	return baseFee, nil
}

// estimateNextBaseFee calculates what the base fee should be on the next block if it
// were produced immediately. If the current time is less than the timestamp of the latest
// block, this esimtate uses the timestamp of the latest block instead.
// If the latest block has a nil base fee, this function will return nil as the base fee
// of the next block.
func (oracle *Oracle) estimateNextBaseFee(ctx context.Context) (*big.Int, error) {
	// Fetch the most recent header by number
	header, err := oracle.backend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
	if err != nil {
		return nil, err
	}
	// If the fetched block does not have a base fee, return nil as the base fee
	if header.BaseFee == nil {
		return nil, nil
	}

	// If the block does have a baseFee, calculate the next base fee
	// based on the current time and add it to the tip to estimate the
	// total gas price estimate.
	_, nextBaseFee, err := dummy.EstimateNextBaseFee(oracle.backend.ChainConfig(), header, oracle.clock.Unix())
	return nextBaseFee, err
}

// SuggestPrice returns an estimated price for legacy transactions.
func (oracle *Oracle) SuggestPrice(ctx context.Context) (*big.Int, error) {
	// Estimate the effective tip based on recent blocks.
	tip, baseFee, err := oracle.suggestDynamicFees(ctx)
	if err != nil {
		return nil, err
	}

	// We calculate the [nextBaseFee] if a block were to be produced immediately.
	// If [nextBaseFee] is lower than the estimate from sampling, then we return it
	// to prevent returning an incorrectly high fee when the network is quiescent.
	nextBaseFee, err := oracle.estimateNextBaseFee(ctx)
	if err != nil {
		log.Warn("failed to estimate next base fee", "err", err)
	}
	// Separately from checking the error value, check that [nextBaseFee] is non-nil
	// before attempting to take the minimum.
	if nextBaseFee != nil {
		baseFee = math.BigMin(baseFee, nextBaseFee)
	}

	return new(big.Int).Add(tip, baseFee), nil
}

// SuggestTipCap returns a tip cap so that newly created transaction can have a
// very high chance to be included in the following blocks.
//
// Note, for legacy transactions and the legacy eth_gasPrice RPC call, it will be
// necessary to add the basefee to the returned number to fall back to the legacy
// behavior.
func (oracle *Oracle) SuggestTipCap(ctx context.Context) (*big.Int, error) {
	tip, _, err := oracle.suggestDynamicFees(ctx)
	return tip, err
}

// suggestDynamicFees estimates the gas tip and base fee based on a simple sampling method
func (oracle *Oracle) suggestDynamicFees(ctx context.Context) (*big.Int, *big.Int, error) {
	head, err := oracle.backend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
	if err != nil {
		return nil, nil, err
	}

	headHash := head.Hash()

	// If the latest gasprice is still available, return it.
	oracle.cacheLock.RLock()
	lastHead, lastPrice, lastBaseFee := oracle.lastHead, oracle.lastPrice, oracle.lastBaseFee
	oracle.cacheLock.RUnlock()
	if headHash == lastHead {
		return new(big.Int).Set(lastPrice), new(big.Int).Set(lastBaseFee), nil
	}
	oracle.fetchLock.Lock()
	defer oracle.fetchLock.Unlock()

	// Try checking the cache again, maybe the last fetch fetched what we need
	oracle.cacheLock.RLock()
	lastHead, lastPrice, lastBaseFee = oracle.lastHead, oracle.lastPrice, oracle.lastBaseFee
	oracle.cacheLock.RUnlock()
	if headHash == lastHead {
		return new(big.Int).Set(lastPrice), new(big.Int).Set(lastBaseFee), nil
	}
	var (
		latestBlockNumber     = head.Number.Uint64()
		lowerBlockNumberLimit = uint64(0)
		currentTime           = oracle.clock.Unix()
		tipResults            []*big.Int
		baseFeeResults        []*big.Int
	)

	if uint64(oracle.checkBlocks) <= latestBlockNumber {
		lowerBlockNumberLimit = latestBlockNumber - uint64(oracle.checkBlocks)
	}

	// Process block headers in the range calculated for this gas price estimation.
	for i := latestBlockNumber; i > lowerBlockNumberLimit; i-- {
		feeInfo, err := oracle.getFeeInfo(ctx, i)
		if err != nil {
			return new(big.Int).Set(lastPrice), new(big.Int).Set(lastBaseFee), err
		}

		if feeInfo.timestamp+oracle.maxLookbackSeconds < currentTime {
			break
		}

		if feeInfo.tip != nil {
			tipResults = append(tipResults, feeInfo.tip)
		} else {
			tipResults = append(tipResults, new(big.Int).Set(common.Big0))
		}

		if feeInfo.baseFee != nil {
			baseFeeResults = append(baseFeeResults, feeInfo.baseFee)
		} else {
			baseFeeResults = append(baseFeeResults, new(big.Int).Set(common.Big0))
		}
	}

	price := lastPrice
	baseFee := lastBaseFee
	if len(tipResults) > 0 {
		sort.Sort(bigIntArray(tipResults))
		price = tipResults[(len(tipResults)-1)*oracle.percentile/100]
	}

	if len(baseFeeResults) > 0 {
		sort.Sort(bigIntArray(baseFeeResults))
		baseFee = baseFeeResults[(len(baseFeeResults)-1)*oracle.percentile/100]
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
	oracle.lastBaseFee = baseFee
	oracle.cacheLock.Unlock()

	return new(big.Int).Set(price), new(big.Int).Set(baseFee), nil
}

// getFeeInfo calculates the minimum required tip to be included in a given
// block and returns the value as a feeInfo struct.
func (oracle *Oracle) getFeeInfo(ctx context.Context, number uint64) (*feeInfo, error) {
	feeInfo, ok := oracle.feeInfoProvider.get(number)
	if ok {
		return feeInfo, nil
	}

	// on cache miss, read from database
	header, err := oracle.backend.HeaderByNumber(ctx, rpc.BlockNumber(number))
	if err != nil {
		return nil, err
	}
	return oracle.feeInfoProvider.addHeader(ctx, header)
}

type bigIntArray []*big.Int

func (s bigIntArray) Len() int           { return len(s) }
func (s bigIntArray) Less(i, j int) bool { return s[i].Cmp(s[j]) < 0 }
func (s bigIntArray) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
