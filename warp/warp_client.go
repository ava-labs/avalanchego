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

var _ Client = (*client)(nil)

type Client interface {
	// GetSignature requests the BLS signature associated with a messageID
	GetSignature(ctx context.Context, messageID ids.ID) ([]byte, error)
	// GetAggregateSignature requests the aggregate signature associated with messageID
	GetAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64) ([]byte, error)
}

// client implementation for interacting with EVM [chain]
type client struct {
	client *rpc.Client
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) (Client, error) {
	innerClient, err := rpc.Dial(fmt.Sprintf("%s/ext/bc/%s/rpc", uri, chain))
	if err != nil {
		return nil, fmt.Errorf("failed to dial client. err: %w", err)
	}
	return &client{
		client: innerClient,
	}, nil
}

func (c *client) GetSignature(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getSignature", messageID); err != nil {
		return nil, fmt.Errorf("call to warp_getSignature failed. err: %w", err)
	}
	return res, nil
}

func (c *client) GetAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getAggregateSignature", messageID, quorumNum); err != nil {
		return nil, fmt.Errorf("call to warp_getAggregateSignature failed. err: %w", err)
	}
	return res, nil
}
