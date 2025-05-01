// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"
)

type ethCustomClient struct {
	c *rpc.Client
}

func newCustomEthClient(ctx context.Context, rawurl string) (*ethCustomClient, error) {
	c, err := rpc.DialContext(ctx, rawurl)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", rawurl, err)
	}
	return &ethCustomClient{
		c: c,
	}, nil
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were created
// immediately. There is no guarantee that this will be the base fee used in the next block
// or that the next base fee will be higher or lower than the returned value.
func (ec *ethCustomClient) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := ec.c.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}
