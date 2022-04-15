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

package rpcdb

import (
	"net"
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/memdb"

	rpcdbpb "github.com/chain4travel/caminogo/proto/pb/rpcdb"
)

const (
	bufSize = 1024 * 1024
)

func setupDB(t testing.TB) (database.Database, func()) {
	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	rpcdbpb.RegisterDatabaseServer(server, NewServer(memdb.New()))
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	dialer := grpc.WithContextDialer(
		func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		},
	)

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "", dialer, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to dial: %s", err)
	}

	db := NewClient(rpcdbpb.NewDatabaseClient(conn))

	close := func() {
		server.Stop()
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
