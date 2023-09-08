// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

type testDatabase struct {
	client  *DatabaseClient
	server  *memdb.Database
	closeFn func()
}

func setupDB(t testing.TB) *testDatabase {
	require := require.New(t)

	db := &testDatabase{
		server: memdb.New(),
	}

	listener, err := grpcutils.NewListener()
	require.NoError(err)
	serverCloser := grpcutils.ServerCloser{}

	server := grpcutils.NewServer()
	rpcdbpb.RegisterDatabaseServer(server, NewServer(db.server))
	serverCloser.Add(server)

	go grpcutils.Serve(listener, server)

	conn, err := grpcutils.Dial(listener.Addr().String())
	require.NoError(err)

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

func FuzzKeyValue(f *testing.F) {
	db := setupDB(f)
	database.FuzzKeyValue(f, db.client)

	db.closeFn()
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	db := setupDB(f)
	database.FuzzNewIteratorWithPrefix(f, db.client)

	db.closeFn()
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
			require := require.New(t)

			baseDB := setupDB(t)
			db := corruptabledb.New(baseDB.server)
			defer db.Close()
			require.NoError(scenario.testFn(db))

			// check db HealthCheck
			_, err := db.HealthCheck(context.Background())
			if scenario.wantErr {
				require.Error(err) //nolint:forbidigo
				require.Contains(err.Error(), scenario.wantErrMsg)
				return
			}
			require.NoError(err)

			// check rpc HealthCheck
			_, err = baseDB.client.HealthCheck(context.Background())
			require.NoError(err)
		})
	}
}
