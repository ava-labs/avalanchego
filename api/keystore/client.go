// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = (*client)(nil)

// Client interface for Avalanche Keystore API Endpoint
//
// Deprecated: The Keystore API is deprecated. Dedicated wallets should be used
// instead.
type Client interface {
	CreateUser(context.Context, api.UserPass, ...rpc.Option) error
	// Returns the usernames of all keystore users
	ListUsers(context.Context, ...rpc.Option) ([]string, error)
	// Returns the byte representation of the given user
	ExportUser(context.Context, api.UserPass, ...rpc.Option) ([]byte, error)
	// Import [exportedUser] to [importTo]
	ImportUser(ctx context.Context, importTo api.UserPass, exportedUser []byte, options ...rpc.Option) error
	// Delete the given user
	DeleteUser(context.Context, api.UserPass, ...rpc.Option) error
}

// Client implementation for Avalanche Keystore API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// Deprecated: The Keystore API is deprecated. Dedicated wallets should be used
// instead.
func NewClient(uri string) Client {
	return &client{requester: rpc.NewEndpointRequester(
		uri + "/ext/keystore",
	)}
}

func (c *client) CreateUser(ctx context.Context, user api.UserPass, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "keystore.createUser", &user, &api.EmptyReply{}, options...)
}

func (c *client) ListUsers(ctx context.Context, options ...rpc.Option) ([]string, error) {
	res := &ListUsersReply{}
	err := c.requester.SendRequest(ctx, "keystore.listUsers", struct{}{}, res, options...)
	return res.Users, err
}

func (c *client) ExportUser(ctx context.Context, user api.UserPass, options ...rpc.Option) ([]byte, error) {
	res := &ExportUserReply{
		Encoding: formatting.Hex,
	}
	err := c.requester.SendRequest(ctx, "keystore.exportUser", &user, res, options...)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.User)
}

func (c *client) ImportUser(ctx context.Context, user api.UserPass, account []byte, options ...rpc.Option) error {
	accountStr, err := formatting.Encode(formatting.Hex, account)
	if err != nil {
		return err
	}

	return c.requester.SendRequest(ctx, "keystore.importUser", &ImportUserArgs{
		UserPass: user,
		User:     accountStr,
		Encoding: formatting.Hex,
	}, &api.EmptyReply{}, options...)
}

func (c *client) DeleteUser(ctx context.Context, user api.UserPass, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "keystore.deleteUser", &user, &api.EmptyReply{}, options...)
}
