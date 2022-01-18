// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for Avalanche Keystore API Endpoint
type Client interface {
	CreateUser(context.Context, api.UserPass) (bool, error)
	// Returns the usernames of all keystore users
	ListUsers(context.Context) ([]string, error)
	// Returns the byte representation of the given user
	ExportUser(context.Context, api.UserPass) ([]byte, error)
	// Import [exportedUser] to [importTo]
	ImportUser(ctx context.Context, importTo api.UserPass, exportedUser []byte) (bool, error)
	// Delete the given user
	DeleteUser(context.Context, api.UserPass) (bool, error)
}

// Client implementation for Avalanche Keystore API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/keystore", "keystore"),
	}
}

func (c *client) CreateUser(ctx context.Context, user api.UserPass) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "createUser", &user, res)
	return res.Success, err
}

func (c *client) ListUsers(ctx context.Context) ([]string, error) {
	res := &ListUsersReply{}
	err := c.requester.SendRequest(ctx, "listUsers", struct{}{}, res)
	return res.Users, err
}

func (c *client) ExportUser(ctx context.Context, user api.UserPass) ([]byte, error) {
	res := &ExportUserReply{
		Encoding: formatting.Hex,
	}
	err := c.requester.SendRequest(ctx, "exportUser", &user, res)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.User)
}

func (c *client) ImportUser(ctx context.Context, user api.UserPass, account []byte) (bool, error) {
	accountStr, err := formatting.EncodeWithChecksum(formatting.Hex, account)
	if err != nil {
		return false, err
	}

	res := &api.SuccessResponse{}
	err = c.requester.SendRequest(ctx, "importUser", &ImportUserArgs{
		UserPass: user,
		User:     accountStr,
		Encoding: formatting.Hex,
	}, res)
	return res.Success, err
}

func (c *client) DeleteUser(ctx context.Context, user api.UserPass) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "deleteUser", &user, res)
	return res.Success, err
}
