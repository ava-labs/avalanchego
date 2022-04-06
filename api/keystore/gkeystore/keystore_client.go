// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/database/rpcdb"

	keystorepb "github.com/ava-labs/avalanchego/proto/pb/keystore"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var _ keystore.BlockchainKeystore = &Client{}

// Client is a snow.Keystore that talks over RPC.
type Client struct {
	client keystorepb.KeystoreClient
	broker *plugin.GRPCBroker
}

// NewClient returns a keystore instance connected to a remote keystore instance
func NewClient(client keystorepb.KeystoreClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: client,
		broker: broker,
	}
}

func (c *Client) GetDatabase(username, password string) (*encdb.Database, error) {
	bcDB, err := c.GetRawDatabase(username, password)
	if err != nil {
		return nil, err
	}
	return encdb.New([]byte(password), bcDB)
}

func (c *Client) GetRawDatabase(username, password string) (database.Database, error) {
	resp, err := c.client.GetDatabase(context.Background(), &keystorepb.GetDatabaseRequest{
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

	dbClient := rpcdb.NewClient(rpcdbpb.NewDatabaseClient(dbConn))
	return dbClient, err
}
