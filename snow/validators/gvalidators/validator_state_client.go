// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

var _ validators.State = (*Client)(nil)

type Client struct {
	client pb.ValidatorStateClient
}

func NewClient(client pb.ValidatorStateClient) *Client {
	return &Client{client: client}
}

func (c *Client) GetMinimumHeight() (uint64, error) {
	resp, err := c.client.GetMinimumHeight(context.Background(), &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetCurrentHeight() (uint64, error) {
	resp, err := c.client.GetCurrentHeight(context.Background(), &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	resp, err := c.client.GetValidatorSet(context.Background(), &pb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}

	vdrs := make(map[ids.NodeID]uint64, len(resp.Validators))
	for _, validator := range resp.Validators {
		nodeID, err := ids.ToNodeID(validator.NodeId)
		if err != nil {
			return nil, err
		}
		vdrs[nodeID] = validator.Weight
	}
	return vdrs, nil
}
