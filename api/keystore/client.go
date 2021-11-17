// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for Avalanche Keystore API Endpoint
type Client interface {
	CreateUser(api.UserPass) (bool, error)
	// Returns the usernames of all keystore users
	ListUsers() ([]string, error)
	// Returns the byte representation of the given user
	ExportUser(api.UserPass) ([]byte, error)
	// Import [exportedUser] to [importTo]
	ImportUser(importTo api.UserPass, exportedUser []byte) (bool, error)
	// Delete the given user
	DeleteUser(api.UserPass) (bool, error)
}

// Client implementation for Avalanche Keystore API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

func NewClient(uri string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/keystore", "keystore", requestTimeout),
	}
}

func (c *client) CreateUser(user api.UserPass) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("createUser", &user, res)
	return res.Success, err
}

func (c *client) ListUsers() ([]string, error) {
	res := &ListUsersReply{}
	err := c.requester.SendRequest("listUsers", struct{}{}, res)
	return res.Users, err
}

func (c *client) ExportUser(user api.UserPass) ([]byte, error) {
	res := &ExportUserReply{
		Encoding: formatting.Hex,
	}
	err := c.requester.SendRequest("exportUser", &user, res)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.User)
}

func (c *client) ImportUser(user api.UserPass, account []byte) (bool, error) {
	accountStr, err := formatting.EncodeWithChecksum(formatting.Hex, account)
	if err != nil {
		return false, err
	}

	res := &api.SuccessResponse{}
	err = c.requester.SendRequest("importUser", &ImportUserArgs{
		UserPass: user,
		User:     accountStr,
		Encoding: formatting.Hex,
	}, res)
	return res.Success, err
}

func (c *client) DeleteUser(user api.UserPass) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("deleteUser", &user, res)
	return res.Success, err
}
