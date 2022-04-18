// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	keystorepb "github.com/ava-labs/avalanchego/proto/pb/keystore"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var _ keystore.BlockchainKeystore = &Client{}

// Client is a snow.Keystore that talks over RPC.
type Client struct {
	client keystorepb.KeystoreClient
}

// NewClient returns a keystore instance connected to a remote keystore instance
func NewClient(client keystorepb.KeystoreClient) *Client {
	return &Client{
		client: client,
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

	clientConn, err := grpcutils.Dial(resp.ServerAddr)
	if err != nil {
		return nil, err
	}

	dbClient := rpcdb.NewClient(rpcdbpb.NewDatabaseClient(clientConn))
	return dbClient, err
}
