// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"net"
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

const (
	bufSize = 1024 * 1024
)

func setupDB(t testing.TB) (database.Database, func()) {
	listener := bufconn.Listen(bufSize)
	serverCloser := grpcutils.ServerCloser{}

	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		rpcdbpb.RegisterDatabaseServer(server, NewServer(memdb.New()))
		serverCloser.Add(server)
		return server
	}

	go grpcutils.Serve(listener, serverFunc)

	dialer := grpc.WithContextDialer(
		func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		},
	)

	dopts := grpcutils.DefaultDialOptions
	dopts = append(dopts, dialer)
	conn, err := grpcutils.Dial("", dopts...)
	if err != nil {
		t.Fatalf("Failed to dial: %s", err)
	}

	db := NewClient(rpcdbpb.NewDatabaseClient(conn))

	close := func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
	return db, close
}

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db, close := setupDB(t)
		test(t, db)

		close()
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db, close := setupDB(b)
			bench(b, db, "rpcdb", keys, values)

			close()
		}
	}
}
