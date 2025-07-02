// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package greader

import (
	"context"
	"io"
	"net"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
)

// TestErrIOEOF tests that if an io.EOF is returned by an io.Reader, it
// propagates that same error type.
func TestErrIOEOF(t *testing.T) {
	require := require.New(t)

	server := grpc.NewServer()
	listener := bufconn.Listen(1024)

	readerServer := NewServer(iotest.ErrReader(io.EOF))
	reader.RegisterReaderServer(server, readerServer)

	go func() {
		require.NoError(server.Serve(listener))
	}()

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(err)

	client := NewClient(reader.NewReaderClient(conn))

	buf := make([]byte, 1)
	n, err := client.Read(buf)
	require.Zero(n)
	// Do not use require.ErrorIs because callers use equality checks on io.EOF
	require.Equal(io.EOF, err)
}
