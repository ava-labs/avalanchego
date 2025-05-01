// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"
)

type ethClient struct {
	*ethclient.Client
	c *rpc.Client
}

func newEthClient(ctx context.Context, rawurl string) (*ethClient, error) {
	c, err := rpc.DialContext(ctx, rawurl)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", rawurl, err)
	}
	return &ethClient{
		Client: ethclient.NewClient(c),
		c:      c,
	}, nil
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were created
// immediately. There is no guarantee that this will be the base fee used in the next block
// or that the next base fee will be higher or lower than the returned value.
func (ec *ethClient) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := ec.c.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}
