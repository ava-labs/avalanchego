// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

type testDatabase struct {
	client *DatabaseClient
	server *memdb.Database
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

	t.Cleanup(func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	return db
}

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			db := setupDB(t)
			test(t, db.client)
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	db := setupDB(f)
	dbtest.FuzzKeyValue(f, db.client)
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	db := setupDB(f)
	dbtest.FuzzNewIteratorWithPrefix(f, db.client)
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	db := setupDB(f)
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, db.client)
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("rpcdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := setupDB(b)
				bench(b, db.client, keys, values)
			})
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
			db := corruptabledb.New(baseDB.server, logging.NoLog{})
			defer db.Close()
			require.NoError(scenario.testFn(db))

			// check db HealthCheck
			_, err := db.HealthCheck(t.Context())
			if scenario.wantErr {
				require.Error(err) //nolint:forbidigo
				require.Contains(err.Error(), scenario.wantErrMsg)
				return
			}
			require.NoError(err)

			// check rpc HealthCheck
			_, err = baseDB.client.HealthCheck(t.Context())
			require.NoError(err)
		})
	}
}
