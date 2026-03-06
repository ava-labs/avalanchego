// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package gasprice provides gas price statistics and suggestions for timely
// transaction inclusion.
package gasprice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/intmath"
)

// Backend that the [Estimator] depends on for chain data.
type Backend interface {
	ResolveBlockNumber(bn rpc.BlockNumber) (uint64, error)
	BlockByNumber(bn rpc.BlockNumber) (*types.Block, error)
	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
	LastAcceptedBlock() *blocks.Block
}

// Config allows parameterizing an [Estimator].
type Config struct {
	// Now returns the current time.
	Now func() time.Time

	// MinSuggestedTip is the minimum suggested tip and the default tip if no
	// better estimate can be made.
	MinSuggestedTip *big.Int
	// SuggestedTipPercentile, in the range (0, 100], specifies what percentile of
	// tips is used when suggesting based on recent transactions.
	SuggestedTipPercentile uint64
	MaxSuggestedTip        *big.Int

	// SuggestedTipMaxBlocks specifies the maximum number of recent blocks to fetch
	// for [Estimator.SuggestTipCap].
	SuggestedTipMaxBlocks uint64
	// SuggestedTipMaxDuration specifies how long a block is considered recent
	// for [Estimator.SuggestGasTipCap].
	SuggestedTipMaxDuration time.Duration

	// HistoryMaxBlocksFromHead specifies the furthest lastBlock behind the last
	// accepted block that can be requested by [Estimator.FeeHistory].
	HistoryMaxBlocksFromHead uint64
	// HistoryMaxBlocks specifies the maximum number of blocks that can be
	// fetched in a single call to [Estimator.FeeHistory].
	HistoryMaxBlocks uint64
}

// DefaultConfig returns a [Config] with all fields set to their default values.
func DefaultConfig() Config {
	return Config{
		Now:             time.Now,
		MinSuggestedTip: big.NewInt(1 * params.Wei),
		// SuggestedTipPercentile is chosen to be a value that is lower than the median of the tips in the recent blocks.
		// This is to prevent suggesting a tip that could cause a self-induced fee spiral.
		SuggestedTipPercentile:  40,
		MaxSuggestedTip:         big.NewInt(150 * params.Wei),
		SuggestedTipMaxBlocks:   20,
		SuggestedTipMaxDuration: time.Minute,
		// Chosen to be larger than the fee lookback window that MetaMask uses (20k blocks).
		HistoryMaxBlocksFromHead: 25_000,
		HistoryMaxBlocks:         2048,
	}
}

var (
	errNilNow             = errors.New("config Now must be non-nil")
	errNilMinSuggestedTip = errors.New("config MinSuggestedTip must be non-nil")
	errNilMaxSuggestedTip = errors.New("config MaxSuggestedTip must be non-nil")
	errBadTipPercentile   = errors.New("config SuggestedTipPercentile must be in (0, 100]")
	errMinTipExceedsMax   = errors.New("config MinSuggestedTip must be <= MaxSuggestedTip")
)

// validate returns an error if the config is invalid.
func (c *Config) validate() error {
	switch {
	case c.Now == nil:
		return errNilNow
	case c.MinSuggestedTip == nil:
		return errNilMinSuggestedTip
	case c.MaxSuggestedTip == nil:
		return errNilMaxSuggestedTip
	case c.SuggestedTipPercentile == 0 || c.SuggestedTipPercentile > 100:
		return errBadTipPercentile
	case c.MinSuggestedTip.Cmp(c.MaxSuggestedTip) > 0:
		return errMinTipExceedsMax
	}
	return nil
}

type last struct {
	lock   sync.RWMutex
	number uint64
	price  *big.Int
}

// Estimator provides gas-price suggestions and fee-history data for SAE by
// analyzing recently accepted blocks.
type Estimator struct {
	backend Backend
	c       Config

	last last

	chainHead  event.Subscription
	blockCache *blockCache
}

// NewEstimator creates an Estimator for gas tips and fee history.
func NewEstimator(backend Backend, log logging.Logger, c Config) (*Estimator, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	// New blocks are cached in the background to avoid slow responses after
	// long periods of no requests to the estimator. This allows us to avoid
	// parallelizing reads inside individual API calls.
	//
	// TODO(StephenButtolph): Consider caching upon acceptance rather than execution.
	events := make(chan core.ChainHeadEvent, 1)
	sub := backend.SubscribeChainHeadEvent(events)
	// Additional slots in the cache allows processing queries for previous
	// blocks while new blocks are added concurrently.
	const extraSlots = 5
	size := max(c.SuggestedTipMaxBlocks, c.HistoryMaxBlocksFromHead+c.HistoryMaxBlocks) + extraSlots
	cache := newBlockCache(log, backend, int(size)) //nolint:gosec // Overflow would require misconfiguration
	go func() {
		defer sub.Unsubscribe() // `Unsubscribe` can fire twice on Close(), but it's safe to call multiple times.
		for {
			select {
			case e := <-events:
				cache.cacheBlock(e.Block)
			case <-sub.Err():
				return
			}
		}
	}()

	return &Estimator{
		backend: backend,
		c:       c,
		last: last{
			price: c.MinSuggestedTip,
		},
		chainHead:  sub,
		blockCache: cache,
	}, nil
}

// SuggestGasTipCap recommends a priority-fee (tip) for new transactions based on
// tips from recently accepted transactions.
func (e *Estimator) SuggestGasTipCap(ctx context.Context) (tip *big.Int, _ error) {
	defer func() {
		// Tip is modified by callers of this function, so we must ensure that
		// it is copied.
		if tip != nil {
			tip = new(big.Int).Set(tip)
		}
	}()

	headNumber := e.backend.LastAcceptedBlock().NumberU64()

	e.last.lock.RLock()
	lastNumber, lastPrice := e.last.number, e.last.price
	e.last.lock.RUnlock()
	if headNumber <= lastNumber {
		return lastPrice, nil
	}

	e.last.lock.Lock()
	defer e.last.lock.Unlock()

	// A different goroutine might have beaten us when upgrading to a write lock.
	lastNumber, lastPrice = e.last.number, e.last.price
	if headNumber <= lastNumber {
		return lastPrice, nil
	}

	var (
		newest     = headNumber
		tooOld     = intmath.BoundedSubtract(newest, e.c.SuggestedTipMaxBlocks, 0)
		recentUnix = uint64(e.c.Now().Add(-e.c.SuggestedTipMaxDuration).Unix()) //nolint:gosec // Known non-negative
		tips       []transaction
	)
	for n := newest; n > tooOld; n-- {
		// getBlock does not return an error if the context is cancelled.
		// We don't want to early return from `SuggestGasTipCap` if the context is cancelled.
		// Instead we continue to fetch the blocks and cache them.
		b := e.blockCache.getBlock(n)
		if b == nil || b.timestamp < recentUnix {
			break
		}
		tips = append(tips, b.txs...)
	}

	price := lastPrice
	if n := len(tips); n > 0 {
		slices.SortFunc(tips, transaction.Compare)

		i := (n - 1) * int(e.c.SuggestedTipPercentile) / 100 //nolint:gosec // Known to be between (0, 100]
		price = tips[i].tip
		price = math.BigMax(price, e.c.MinSuggestedTip)
		price = math.BigMin(price, e.c.MaxSuggestedTip)
	}

	e.last.number = headNumber
	e.last.price = price
	return price, nil
}

var (
	errHistoryDepthExhausted  = errors.New("requested block is too far behind accepted head")
	errMissingBlock           = errors.New("missing block")
	errMissingWorstCaseBounds = errors.New("head block does not have worst-case bounds")
)

// FeeHistory returns data relevant for fee estimation based on the specified
// range of blocks.
//
// The range can be specified either with absolute block numbers or ending with
// the latest or pending block.
//
// This function returns:
//
//   - The first block of the actually processed range.
//   - The tips paid for each percentile of the cumulative gas limits of the
//     transactions in each block.
//   - The baseFee of each block and the next block's upperbound baseFee.
//   - The portion that each block was filled.
func (e *Estimator) FeeHistory(
	ctx context.Context,
	blocks uint64,
	lastBlock rpc.BlockNumber,
	rewardPercentiles []float64,
) (
	lowestHeight *big.Int,
	rewards [][]*big.Int,
	baseFees []*big.Int,
	portionFull []float64,
	_ error,
) {
	if err := validatePercentiles(rewardPercentiles); err != nil {
		return nil, nil, nil, nil, err
	}
	last, err := e.backend.ResolveBlockNumber(lastBlock)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	headBlock := e.backend.LastAcceptedBlock()
	head := headBlock.NumberU64()
	if minLast := intmath.BoundedSubtract(head, e.c.HistoryMaxBlocksFromHead, 0); last < minLast {
		return nil, nil, nil, nil, fmt.Errorf("%w: block %d requested, accepted head is %d (max depth %d)",
			errHistoryDepthExhausted,
			last,
			head,
			e.c.HistoryMaxBlocksFromHead,
		)
	}
	blocks = min(
		blocks,               // requested value
		e.c.HistoryMaxBlocks, // DoS protection
		last+1,               // Underflow protection for "first" calculation
	)
	if blocks == 0 {
		return big.NewInt(0), nil, nil, nil, nil
	}

	first := last + 1 - blocks
	var reward [][]*big.Int
	if len(rewardPercentiles) != 0 {
		reward = make([][]*big.Int, 0, blocks)
	}
	var (
		baseFee      = make([]*big.Int, 0, blocks+1)
		gasUsedRatio = make([]float64, 0, blocks)
	)
	for n := first; n <= last; n++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		b := e.blockCache.getBlock(n)
		if b == nil {
			return nil, nil, nil, nil, fmt.Errorf("%w: %d", errMissingBlock, n)
		}

		if len(rewardPercentiles) != 0 {
			reward = append(reward, b.tipPercentiles(rewardPercentiles))
		}
		baseFee = append(baseFee, b.baseFee)
		gasUsedRatio = append(gasUsedRatio, float64(b.gasUsed)/float64(b.gasLimit))
	}
	if last == head {
		bounds := headBlock.WorstCaseBounds()
		if bounds == nil {
			return nil, nil, nil, nil, errMissingWorstCaseBounds
		}
		baseFee = append(baseFee, bounds.LatestEndTime.BaseFee().ToBig())
	} else if b := e.blockCache.getBlock(last + 1); b != nil {
		baseFee = append(baseFee, b.baseFee)
	} else {
		return nil, nil, nil, nil, fmt.Errorf("%w: %d", errMissingBlock, last+1)
	}
	return new(big.Int).SetUint64(first), reward, baseFee, gasUsedRatio, nil
}

var _ io.Closer = (*Estimator)(nil)

// Close releases allocated resources.
func (e *Estimator) Close() error {
	e.chainHead.Unsubscribe()
	return nil
}

const maxPercentiles = 100

var errBadPercentile = errors.New("percentile out of range or misordered")

func validatePercentiles(percentiles []float64) error {
	if len(percentiles) > maxPercentiles {
		return fmt.Errorf("%w: requested %d percentiles, max %d", errBadPercentile, len(percentiles), maxPercentiles)
	}
	for i, p := range percentiles {
		if p < 0 || p > 100 {
			return fmt.Errorf("%w: value %f at index %d", errBadPercentile, p, i)
		}
		if i > 0 && p <= percentiles[i-1] {
			return fmt.Errorf("%w: index %d (%f) must be greater than index %d (%f)", errBadPercentile, i, p, i-1, percentiles[i-1])
		}
	}
	return nil
}
