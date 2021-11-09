// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	ListUsers() ([]string, error)
	ExportUser(api.UserPass) ([]byte, error)
	ImportUser(api.UserPass, []byte) (bool, error)
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

// ListUsers lists the usernames of all keystore users on the node
func (c *client) ListUsers() ([]string, error) {
	res := &ListUsersReply{}
	err := c.requester.SendRequest("listUsers", struct{}{}, res)
	return res.Users, err
}

// ExportUser returns the byte representation of the requested [user]
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

// ImportUser imports the keystore user in [account] under [user]
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

// DeleteUser removes [user] from the node's keystore users
func (c *client) DeleteUser(user api.UserPass) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("deleteUser", &user, res)
	return res.Success, err
}
