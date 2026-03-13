// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common/hexutil"
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
	panic(errUnimplemented)
}

// BaseFee returns the worst-case base fee of the last accepted block.
func (c *customAPI) BaseFee(ctx context.Context) (*hexutil.Big, error) {
	panic(errUnimplemented)
}

// detailedExecutionResult is the response for eth_callDetailed.
type detailedExecutionResult struct {
	UsedGas    uint64        `json:"gas"`
	ErrCode    int           `json:"errCode"`
	Err        string        `json:"err"`
	ReturnData hexutil.Bytes `json:"returnData"`
}

// CallDetailed performs the same call as eth_call, but returns gas usage and
// error details instead of just the return data.
func (c *customAPI) CallDetailed(ctx context.Context, args any, blockNrOrHash rpc.BlockNumberOrHash, overrides any) (*detailedExecutionResult, error) {
	panic(errUnimplemented)
}

// price represents a single gas-price suggestion.
type price struct {
	GasTip *hexutil.Big `json:"maxPriorityFeePerGas"`
	GasFee *hexutil.Big `json:"maxFeePerGas"`
}

// priceOptions groups slow/normal/fast gas-price suggestions.
type priceOptions struct {
	Slow   *price `json:"slow"`
	Normal *price `json:"normal"`
	Fast   *price `json:"fast"`
}

// SuggestPriceOptions returns gas-price suggestions at three speed tiers.
func (c *customAPI) SuggestPriceOptions(ctx context.Context) (*priceOptions, error) {
	panic(errUnimplemented)
}

// NewAcceptedTransactions creates a subscription that is notified each time a
// transaction is accepted by consensus (prior to execution). If fullTx is true
// the full tx is sent to the client, otherwise only the hash is sent.
func (c *customAPI) NewAcceptedTransactions(ctx context.Context, fullTx *bool) (*rpc.Subscription, error) {
	panic(errUnimplemented)
}
