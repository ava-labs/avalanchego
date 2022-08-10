// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

const (
	bufSize = 1024 * 1024
)

type testDatabase struct {
	client  *DatabaseClient
	server  *memdb.Database
	closeFn func()
}

func setupDB(t testing.TB) *testDatabase {
	db := &testDatabase{
		server: memdb.New(),
	}

	listener := bufconn.Listen(bufSize)
	serverCloser := grpcutils.ServerCloser{}

	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		rpcdbpb.RegisterDatabaseServer(server, NewServer(db.server))
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

	db.client = NewClient(rpcdbpb.NewDatabaseClient(conn))
	db.closeFn = func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
	return db
}

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := setupDB(t)
		test(t, db.client)

		db.closeFn()
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db := setupDB(b)
			bench(b, db.client, "rpcdb", keys, values)
			db.closeFn()
		}
	}
}

func TestHealthCheck(t *testing.T) {
	assert := assert.New(t)

	scenarios := []struct {
		name         string
		testDatabase *testDatabase
		testFn       func(db *corruptabledb.Database) error
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:         "healthcheck success",
			testDatabase: setupDB(t),
			testFn: func(_ *corruptabledb.Database) error {
				return nil
			},
		},
		{
			name:         "healthcheck failed db closed",
			testDatabase: setupDB(t),
			testFn: func(db *corruptabledb.Database) error {
				return db.Close()
			},
			wantErr:    true,
			wantErrMsg: "closed",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			baseDB := setupDB(t)
			db := corruptabledb.New(baseDB.server)
			defer db.Close()
			assert.NoError(scenario.testFn(db))

			// check db HealthCheck
			_, err := db.HealthCheck()
			if err == nil && scenario.wantErr {
				t.Fatalf("wanted error got nil")
				return
			}
			if scenario.wantErr {
				assert.Containsf(err.Error(), scenario.wantErrMsg, "expected error containing %q, got %s", scenario.wantErrMsg, err)
				return
			}
			assert.Nil(err)

			// check rpc HealthCheck
			_, err = baseDB.client.HealthCheck()
			if err == nil && scenario.wantErr {
				t.Fatalf("wanted error got nil")
				return
			}
			if scenario.wantErr {
				assert.Containsf(err.Error(), scenario.wantErrMsg, "expected error containing %q, got %s", scenario.wantErrMsg, err)
				return
			}
			assert.Nil(err)
		})
	}
}
