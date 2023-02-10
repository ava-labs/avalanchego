// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/rpc"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var _ WarpClient = (*warpClient)(nil)

type WarpClient interface {
	GetSignature(ctx context.Context, messageID ids.ID) ([]byte, error)
}

// warpClient implementation for interacting with EVM [chain]
type warpClient struct {
	client *rpc.Client
}

// NewWarpClient returns a WarpClient for interacting with EVM [chain]
func NewWarpClient(uri, chain string) (WarpClient, error) {
	client, err := rpc.Dial(fmt.Sprintf("%s/ext/bc/%s/rpc", uri, chain))
	if err != nil {
		return nil, fmt.Errorf("failed to dial client. err: %w", err)
	}
	return &warpClient{
		client: client,
	}, nil
}

// GetSignature requests the BLS signature associated with a messageID
func (c *warpClient) GetSignature(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	err := c.client.CallContext(ctx, &res, "warp_getSignature", messageID)
	if err != nil {
		return nil, fmt.Errorf("call to warp_getSignature failed. err: %w", err)
	}
	return res, err
}
