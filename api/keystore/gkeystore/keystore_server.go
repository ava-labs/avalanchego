// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gkeystore

import (
	"context"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	keystorepb "github.com/ava-labs/avalanchego/proto/pb/keystore"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

var _ keystorepb.KeystoreServer = &Server{}

// Server is a snow.Keystore that is managed over RPC.
type Server struct {
	keystorepb.UnsafeKeystoreServer
	ks keystore.BlockchainKeystore
}

// NewServer returns a keystore connected to a remote keystore
func NewServer(ks keystore.BlockchainKeystore) *Server {
	return &Server{
		ks: ks,
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
	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return nil, err
	}
	serverAddr := serverListener.Addr().String()

	go grpcutils.Serve(serverListener, func(opts []grpc.ServerOption) *grpc.Server {
		if len(opts) == 0 {
			opts = append(opts, grpcutils.DefaultServerOptions...)
		}
		server := grpc.NewServer(opts...)
		closer.closer.Add(server)
		db := rpcdb.NewServer(&closer)
		rpcdbpb.RegisterDatabaseServer(server, db)
		return server
	})
	return &keystorepb.GetDatabaseResponse{ServerAddr: serverAddr}, nil
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
