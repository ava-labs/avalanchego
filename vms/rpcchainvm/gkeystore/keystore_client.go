// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanche-go/database"
	"github.com/ava-labs/avalanche-go/database/rpcdb"
	"github.com/ava-labs/avalanche-go/database/rpcdb/rpcdbproto"
	"github.com/ava-labs/avalanche-go/snow"
	"github.com/ava-labs/avalanche-go/vms/rpcchainvm/gkeystore/gkeystoreproto"
)

var (
	_ snow.Keystore = &Client{}
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client gkeystoreproto.KeystoreClient
	broker *plugin.GRPCBroker
}

// NewClient returns a keystore instance connected to a remote keystore instance
func NewClient(client gkeystoreproto.KeystoreClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: client,
		broker: broker,
	}
}

// GetDatabase ...
func (c *Client) GetDatabase(username, password string) (database.Database, error) {
	resp, err := c.client.GetDatabase(context.Background(), &gkeystoreproto.GetDatabaseRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, err
	}

	dbConn, err := c.broker.Dial(resp.DbServer)
	if err != nil {
		return nil, err
	}

	dbClient := rpcdb.NewClient(rpcdbproto.NewDatabaseClient(dbConn))
	return dbClient, err
}
