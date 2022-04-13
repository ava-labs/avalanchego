// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"github.com/hashicorp/go-plugin"

	"github.com/chain4travel/caminogo/api/keystore"
	"github.com/chain4travel/caminogo/api/proto/gkeystoreproto"
	"github.com/chain4travel/caminogo/api/proto/rpcdbproto"
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/encdb"
	"github.com/chain4travel/caminogo/database/rpcdb"
)

var _ keystore.BlockchainKeystore = &Client{}

// Client is a snow.Keystore that talks over RPC.
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

func (c *Client) GetDatabase(username, password string) (*encdb.Database, error) {
	bcDB, err := c.GetRawDatabase(username, password)
	if err != nil {
		return nil, err
	}
	return encdb.New([]byte(password), bcDB)
}

func (c *Client) GetRawDatabase(username, password string) (database.Database, error) {
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
