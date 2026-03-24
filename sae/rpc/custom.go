// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"math/big"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
)

// customAPI implements Avalanche-custom RPCs. These are not part of the
// standard Ethereum JSON-RPC spec or in geth, but are exposed by Avalanche
// nodes for compatibility with tooling that depends on them (e.g. Core).
//
// Reference implementations live at:
// - https://github.com/ava-labs/avalanchego/blob/v1.14.1/graft/coreth/internal/ethapi/api_extra.go
// - https://github.com/ava-labs/avalanchego/blob/v1.14.1/graft/coreth/internal/ethapi/api.coreth.go
type customAPI struct {
	b *backend
}

// GetChainConfig returns the chain configuration.
func (c *customAPI) GetChainConfig(ctx context.Context) *params.ChainConfig {
	return c.b.ChainConfig()
}

// BaseFee returns an upper-bound estimate of the base fee for the next block.
func (c *customAPI) BaseFee(ctx context.Context) *hexutil.Big {
	return (*hexutil.Big)(c.estimateNextBaseFee())
}

// estimateNextBaseFee returns the worst-case upper bound on the next block's
// base fee. Before any blocks are executed it falls back to the last accepted
// block's base fee.
func (c *customAPI) estimateNextBaseFee() *big.Int {
	bounds := c.b.LastAccepted().WorstCaseBounds()
	if bounds == nil {
		return c.b.LastAccepted().EthBlock().BaseFee()
	}
	return bounds.LatestEndTime.BaseFee().ToBig()
}

// DetailedExecutionResult is the response for eth_callDetailed.
type DetailedExecutionResult struct {
	UsedGas    uint64        `json:"gas"`
	ErrCode    int           `json:"errCode"`
	Err        string        `json:"err"`
	ReturnData hexutil.Bytes `json:"returnData"`
}

// CallDetailed performs the same call as eth_call, but returns gas usage and
// error details instead of just the return data.
func (c *customAPI) CallDetailed(ctx context.Context, args any, blockNrOrHash rpc.BlockNumberOrHash, overrides any) (*DetailedExecutionResult, error) {
	panic(errUnimplemented)
}

// Price represents a single gas-Price suggestion.
type Price struct {
	GasTip *hexutil.Big `json:"maxPriorityFeePerGas"`
	GasFee *hexutil.Big `json:"maxFeePerGas"`
}

// newPrice returns a [price] with the given tip and a max fee of tip + baseFee.
// It allocates new big.Ints so the caller retains ownership of the inputs.
func newPrice(tip, baseFee *big.Int) *Price {
	return &Price{
		GasTip: (*hexutil.Big)(new(big.Int).Set(tip)),
		GasFee: (*hexutil.Big)(new(big.Int).Add(tip, baseFee)),
	}
}

// PriceOptions groups slow/normal/fast gas-price suggestions.
type PriceOptions struct {
	Slow   *Price `json:"slow"`
	Normal *Price `json:"normal"`
	Fast   *Price `json:"fast"`
}

var minGasTip = big.NewInt(params.Wei)

// Tip scaling percentages for gas price options.
const (
	slowTipPercent = 95
	fastTipPercent = 105
)

// NewPriceOptions returns slow, normal, and fast [priceOptions] derived from the given tip and base fee.
// The slow tip is floored at [minGasTip], and normal/fast are floored at the
// previous tier to guarantee slow <= normal <= fast.
func NewPriceOptions(tip, baseFee *big.Int) *PriceOptions {
	slowTip := new(big.Int).Set(math.BigMax(scale(tip, slowTipPercent), minGasTip))
	normalTip := new(big.Int).Set(math.BigMax(tip, slowTip))
	fastTip := new(big.Int).Set(math.BigMax(scale(tip, fastTipPercent), normalTip))
	return &PriceOptions{
		Slow:   newPrice(slowTip, baseFee),
		Normal: newPrice(normalTip, baseFee),
		Fast:   newPrice(fastTip, baseFee),
	}
}

var big100 = big.NewInt(100)

// scale returns v * percent / 100.
func scale(v *big.Int, percent uint64) *big.Int {
	x := new(big.Int).SetUint64(percent)
	x.Mul(x, v)
	return x.Div(x, big100)
}

// SuggestPriceOptions returns gas-price suggestions at three speed tiers.
func (c *customAPI) SuggestPriceOptions(ctx context.Context) (*PriceOptions, error) {
	tip, err := c.b.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, err
	}
	doubleBaseFee := c.estimateNextBaseFee()

	// Double the base fee estimate so the suggested maxFeePerGas remains
	// valid even if the base fee rises for several consecutive
	// blocks before the transaction is included.
	doubleBaseFee.Lsh(doubleBaseFee, 1)
	return NewPriceOptions(tip, doubleBaseFee), nil
}

// NewAcceptedTransactions creates a subscription that is notified each time a
// transaction is accepted by consensus (prior to execution). If fullTx is true
// the full tx is sent to the client, otherwise only the hash is sent.
func (c *customAPI) NewAcceptedTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	panic(errUnimplemented)
}
