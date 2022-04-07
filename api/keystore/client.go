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
	CreateUser(context.Context, api.UserPass, ...rpc.Option) (bool, error)
	// Returns the usernames of all keystore users
	ListUsers(context.Context, ...rpc.Option) ([]string, error)
	// Returns the byte representation of the given user
	ExportUser(context.Context, api.UserPass, ...rpc.Option) ([]byte, error)
	// Import [exportedUser] to [importTo]
	ImportUser(ctx context.Context, importTo api.UserPass, exportedUser []byte, options ...rpc.Option) (bool, error)
	// Delete the given user
	DeleteUser(context.Context, api.UserPass, ...rpc.Option) (bool, error)
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

func (c *client) CreateUser(ctx context.Context, user api.UserPass, options ...rpc.Option) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "createUser", &user, res, options...)
	return res.Success, err
}

func (c *client) ListUsers(ctx context.Context, options ...rpc.Option) ([]string, error) {
	res := &ListUsersReply{}
	err := c.requester.SendRequest(ctx, "listUsers", struct{}{}, res, options...)
	return res.Users, err
}

func (c *client) ExportUser(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]byte, error) {
	res := &ExportUserReply{
		Encoding: formatting.Hex,
	}
	err := c.requester.SendRequest(ctx, "exportUser", &user, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.User)
}

func (c *client) ImportUser(ctx context.Context, user api.UserPass, account []byte, options ...rpc.Option) (bool, error) {
	accountStr, err := formatting.EncodeWithChecksum(formatting.Hex, account)
	if err != nil {
		return false, err
	}

	res := &api.SuccessResponse{}
	err = c.requester.SendRequest(ctx, "importUser", &ImportUserArgs{
		UserPass: user,
		User:     accountStr,
		Encoding: formatting.Hex,
	}, res, options...)
	return res.Success, err
}

func (c *client) DeleteUser(ctx context.Context, user api.UserPass, options ...rpc.Option) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "deleteUser", &user, res, options...)
	return res.Success, err
}
