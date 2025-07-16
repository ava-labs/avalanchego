// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gwarp

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/platformvm/warp"

	pb "github.com/ava-labs/avalanchego/buf/proto/pb/warp"
)

var _ warp.Signer = (*Client)(nil)

type Client struct {
	client pb.SignerClient
}

func NewClient(client pb.SignerClient) *Client {
	return &Client{client: client}
}

func (c *Client) Sign(unsignedMsg *warp.UnsignedMessage) ([]byte, error) {
	resp, err := c.client.Sign(context.Background(), &pb.SignRequest{
		NetworkId:     unsignedMsg.NetworkID,
		SourceChainId: unsignedMsg.SourceChainID[:],
		Payload:       unsignedMsg.Payload,
	})
	if err != nil {
		return nil, err
	}
	return resp.Signature, nil
}
