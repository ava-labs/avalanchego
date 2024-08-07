// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

func (c *client) GetGenesisBytes(ctx context.Context, options ...rpc.Option) ([]byte, error) {
	res := &GetGenesisBytesReply{}
	err := c.requester.SendRequest(ctx, "info.getGenesisBytes", struct{}{}, res, options...)
	return res.GenesisBytes, err
}
