// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gteleporter

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/teleporter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	pb "github.com/ava-labs/avalanchego/proto/pb/teleporter"
)

const bufSize = 1024 * 1024

type testSigner struct {
	client  *Client
	server  teleporter.Signer
	sk      *bls.SecretKey
	chainID ids.ID
	closeFn func()
}

func setupSigner(t testing.TB) *testSigner {
	require := require.New(t)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	chainID := ids.GenerateTestID()

	s := &testSigner{
		server:  teleporter.NewSigner(sk, chainID),
		sk:      sk,
		chainID: chainID,
	}

	listener := bufconn.Listen(bufSize)
	serverCloser := grpcutils.ServerCloser{}

	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		server := grpcutils.NewDefaultServer(opts)
		pb.RegisterSignerServer(server, NewServer(s.server))
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
	require.NoError(err)

	s.client = NewClient(pb.NewSignerClient(conn))
	s.closeFn = func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
	return s
}

func TestInterface(t *testing.T) {
	for _, test := range teleporter.SignerTests {
		s := setupSigner(t)
		test(t, s.client, s.sk, s.chainID)
		s.closeFn()
	}
}
