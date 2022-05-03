// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	aliasreaderpb "github.com/ava-labs/avalanchego/proto/pb/aliasreader"
)

const (
	bufSize = 1024 * 1024
)

func TestInterface(t *testing.T) {
	assert := assert.New(t)

	for _, test := range ids.AliasTests {
		listener := bufconn.Listen(bufSize)
		serverCloser := grpcutils.ServerCloser{}
		w := ids.NewAliaser()

		serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
			server := grpc.NewServer(opts...)
			aliasreaderpb.RegisterAliasReaderServer(server, NewServer(w))
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
		assert.NoError(err)

		r := NewClient(aliasreaderpb.NewAliasReaderClient(conn))
		test(assert, r, w)

		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
}
