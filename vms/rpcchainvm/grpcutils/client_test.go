// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"testing"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/rpcdb"

	pb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

func TestDialOptsSmoke(t *testing.T) {
	require := require.New(t)

	opts := newDialOpts()
	require.Len(opts, 3)

	opts = newDialOpts(
		WithChainUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		WithChainStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
	)
	require.Len(opts, 5)
}

// Test_WaitForReady shows the expected results from the DialOption during
// client creation.  If true the client will block and wait forever for the
// server to become Ready even if the listener is closed.
// ref. https://github.com/grpc/grpc/blob/master/doc/wait-for-ready.md
func TestWaitForReady(t *testing.T) {
	require := require.New(t)

	listener, err := NewListener()
	require.NoError(err)
	defer listener.Close()

	server := NewServer()
	defer server.Stop()
	pb.RegisterDatabaseServer(server, rpcdb.NewServer(memdb.New()))

	go func() {
		time.Sleep(100 * time.Millisecond)
		Serve(listener, server)
	}()

	// The default includes grpc.WaitForReady(true).
	conn, err := Dial(listener.Addr().String())
	require.NoError(err)

	db := rpcdb.NewClient(pb.NewDatabaseClient(conn))

	err = db.Put([]byte("foo"), []byte("bar"))
	require.NoError(err)

	noWaitListener, err := NewListener()
	require.NoError(err)
	// close listener causes RPC to fail fast.
	// The client would timeout otherwise.
	_ = noWaitListener.Close()

	// By directly calling `grpc.Dial` rather than `Dial`, the default does not
	// include setting grpc.WaitForReady(true).
	noWaitConn, err := grpc.Dial(
		noWaitListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)

	db = rpcdb.NewClient(pb.NewDatabaseClient(noWaitConn))

	err = db.Put([]byte("foo"), []byte("bar"))
	status, ok := status.FromError(err)
	require.True(ok)
	require.Equal(codes.Unavailable, status.Code())
}
