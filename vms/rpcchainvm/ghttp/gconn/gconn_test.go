// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	connpb "github.com/ava-labs/avalanchego/proto/pb/net/conn"
)

// TestErrIOEOF tests that if a net.Conn returns an io.EOF, it propagates that
// same error type.
func TestErrIOEOF(t *testing.T) {
	require := require.New(t)

	server := grpc.NewServer()
	listener := bufconn.Listen(1024)

	serverCloser := &grpcutils.ServerCloser{}
	serverCloser.Add(server)

	conn1, conn2 := net.Pipe()
	connServer := NewServer(conn1, serverCloser)

	connpb.RegisterConnServer(server, connServer)

	go func() {
		_ = server.Serve(listener)
	}()

	grpcConn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(err)

	client := NewClient(
		connpb.NewConnClient(grpcConn),
		conn1.LocalAddr(),
		conn1.RemoteAddr(),
	)

	require.NoError(conn2.Close())
	buf := make([]byte, 1)
	n, err := client.Read(buf)
	require.Zero(n)
	require.Equal(io.EOF, err)
}

// ErrDeadlineExceeded tests that if a net.Conn returns an
// os.ErrDeadlineExceeded, it propagates that same error type.
func TestOSErrDeadlineExceeded(t *testing.T) {
	require := require.New(t)

	server := grpc.NewServer()
	listener := bufconn.Listen(1024)

	serverCloser := &grpcutils.ServerCloser{}
	serverCloser.Add(server)

	conn1, _ := net.Pipe()
	connServer := NewServer(conn1, serverCloser)
	require.NoError(conn1.SetDeadline(time.Now()))

	connpb.RegisterConnServer(server, connServer)

	go func() {
		_ = server.Serve(listener)
	}()

	grpcConn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(err)

	client := NewClient(
		connpb.NewConnClient(grpcConn),
		conn1.LocalAddr(),
		conn1.RemoteAddr(),
	)

	buf := make([]byte, 1)
	n, err := client.Read(buf)
	require.Zero(n)
	require.ErrorIs(err, os.ErrDeadlineExceeded)
}
