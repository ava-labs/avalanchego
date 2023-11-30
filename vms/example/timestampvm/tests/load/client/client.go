// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/timestampvm/timestampvm"
)

// Client defines timestampvm client operations.
type Client interface {
	// ProposeBlock submits data for a block
	ProposeBlock(ctx context.Context, data [timestampvm.DataLen]byte) (bool, error)

	// GetBlock fetches the contents of a block
	GetBlock(ctx context.Context, blockID *ids.ID) (uint64, [timestampvm.DataLen]byte, uint64, ids.ID, ids.ID, error)
}

// New creates a new client object.
func New(uri string) Client {
	req := NewEndpointRequester(uri, "timestampvm")
	return &client{req: req}
}

type client struct {
	req *EndpointRequester
}

func (cli *client) ProposeBlock(ctx context.Context, data [timestampvm.DataLen]byte) (bool, error) {
	bytes, err := formatting.Encode(formatting.Hex, data[:])
	if err != nil {
		return false, err
	}

	resp := new(timestampvm.ProposeBlockReply)
	err = cli.req.SendRequest(ctx,
		"proposeBlock",
		&timestampvm.ProposeBlockArgs{Data: bytes},
		resp,
	)
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (cli *client) GetBlock(ctx context.Context, blockID *ids.ID) (uint64, [timestampvm.DataLen]byte, uint64, ids.ID, ids.ID, error) {
	resp := new(timestampvm.GetBlockReply)
	err := cli.req.SendRequest(ctx,
		"getBlock",
		&timestampvm.GetBlockArgs{ID: blockID},
		resp,
	)
	if err != nil {
		return 0, [timestampvm.DataLen]byte{}, 0, ids.Empty, ids.Empty, err
	}
	bytes, err := formatting.Decode(formatting.Hex, resp.Data)
	if err != nil {
		return 0, [timestampvm.DataLen]byte{}, 0, ids.Empty, ids.Empty, err
	}
	return uint64(resp.Timestamp), timestampvm.BytesToData(bytes), uint64(resp.Height), resp.ID, resp.ParentID, nil
}
