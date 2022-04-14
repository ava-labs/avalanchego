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

	"google.golang.org/grpc"

	"github.com/chain4travel/caminogo/api/keystore"
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/rpcdb"
	"github.com/chain4travel/caminogo/vms/rpcchainvm/grpcutils"

	keystorepb "github.com/chain4travel/caminogo/proto/pb/keystore"
	rpcdbpb "github.com/chain4travel/caminogo/proto/pb/rpcdb"
)

var _ keystorepb.KeystoreServer = &Server{}

// Server is a snow.Keystore that is managed over RPC.
type Server struct {
	keystorepb.UnimplementedKeystoreServer
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
