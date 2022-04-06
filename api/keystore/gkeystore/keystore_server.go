// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	keystorepb "github.com/ava-labs/avalanchego/proto/pb/keystore"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var _ keystorepb.KeystoreServer = &Server{}

// Server is a snow.Keystore that is managed over RPC.
type Server struct {
	keystorepb.UnimplementedKeystoreServer
	ks     keystore.BlockchainKeystore
	broker *plugin.GRPCBroker
}

// NewServer returns a keystore connected to a remote keystore
func NewServer(ks keystore.BlockchainKeystore, broker *plugin.GRPCBroker) *Server {
	return &Server{
		ks:     ks,
		broker: broker,
	}
}

func (s *Server) GetDatabase(
	_ context.Context,
	req *keystorepb.GetDatabaseRequest,
) (*keystorepb.GetDatabaseResponse, error) {
	db, err := s.ks.GetRawDatabase(req.Username, req.Password)
	if err != nil {
		return nil, err
	}

	closer := dbCloser{Database: db}

	// start the db server
	dbBrokerID := s.broker.NextId()
	go s.broker.AcceptAndServe(dbBrokerID, func(opts []grpc.ServerOption) *grpc.Server {
		opts = append(opts,
			grpc.MaxRecvMsgSize(math.MaxInt),
			grpc.MaxSendMsgSize(math.MaxInt),
		)
		server := grpc.NewServer(opts...)
		closer.closer.Add(server)
		db := rpcdb.NewServer(&closer)
		rpcdbpb.RegisterDatabaseServer(server, db)
		return server
	})
	return &keystorepb.GetDatabaseResponse{DbServer: dbBrokerID}, nil
}

type dbCloser struct {
	database.Database
	closer grpcutils.ServerCloser
}

func (db *dbCloser) Close() error {
	err := db.Database.Close()
	db.closer.Stop()
	return err
}
