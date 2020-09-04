// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/database/rpcdb"
	"github.com/ava-labs/gecko/database/rpcdb/rpcdbproto"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/rpcchainvm/gkeystore/gkeystoreproto"
)

// Server is a messenger that is managed over RPC.
type Server struct {
	ks     snow.Keystore
	broker *plugin.GRPCBroker
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(ks snow.Keystore, broker *plugin.GRPCBroker) *Server {
	return &Server{
		ks:     ks,
		broker: broker,
	}
}

// GetDatabase ...
func (s *Server) GetDatabase(
	_ context.Context,
	req *gkeystoreproto.GetDatabaseRequest,
) (*gkeystoreproto.GetDatabaseResponse, error) {
	db, err := s.ks.GetDatabase(req.Username, req.Password)
	if err != nil {
		return nil, err
	}

	// start the db server
	dbBrokerID := s.broker.NextId()
	go s.broker.AcceptAndServe(dbBrokerID, func(opts []grpc.ServerOption) *grpc.Server {
		db := rpcdb.NewServer(db)
		server := grpc.NewServer(opts...)
		rpcdbproto.RegisterDatabaseServer(server, db)
		return server
	})

	return &gkeystoreproto.GetDatabaseResponse{
		DbServer: dbBrokerID,
	}, nil
}
