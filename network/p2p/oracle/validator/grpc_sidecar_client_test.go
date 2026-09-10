// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"net"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

// stubSidecarServer is a test double for the gRPC OracleSidecar service.
type stubSidecarServer struct {
	pb.UnimplementedOracleSidecarServer
	verifyF func(ctx context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error)
}

func (s *stubSidecarServer) Verify(ctx context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	if s.verifyF != nil {
		return s.verifyF(ctx, req)
	}
	return &pb.VerifyResponse{}, nil
}

// startStubServer starts an in-process gRPC server and returns a connected
// GRPCSidecarClient. The server is stopped via t.Cleanup.
func startStubServer(t *testing.T, stub *stubSidecarServer) *GRPCSidecarClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	pb.RegisterOracleSidecarServer(srv, stub)
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return &GRPCSidecarClient{client: pb.NewOracleSidecarClient(conn)}
}

func newTestMsg(t *testing.T) *oracle.OracleMessage {
	t.Helper()
	msg, err := oracle.NewOracleMessage("solana", "addr1", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)
	return msg
}

// TestGRPCSidecarClient_Accept verifies that the client sends raw bytes
// (not hex-encoded) and that the server receives them correctly.
func TestGRPCSidecarClient_Accept(t *testing.T) {
	msg := newTestMsg(t)
	justification := []byte("sig-bytes")

	var gotReq *pb.VerifyRequest
	c := startStubServer(t, &stubSidecarServer{
		verifyF: func(_ context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
			gotReq = req
			return &pb.VerifyResponse{}, nil
		},
	})

	require.NoError(t, c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       msg,
		Justification: justification,
	}))

	require.Equal(t, msg.Bytes(), gotReq.MessageBytes)
	require.Equal(t, justification, gotReq.Justification)
}

func TestGRPCSidecarClient_RejectWithError(t *testing.T) {
	c := startStubServer(t, &stubSidecarServer{
		verifyF: func(_ context.Context, _ *pb.VerifyRequest) (*pb.VerifyResponse, error) {
			return nil, status.Errorf(codes.InvalidArgument, "bad event")
		},
	})

	err := c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorIs(t, err, ErrSidecarRejected)
	require.Contains(t, err.Error(), "bad event")
}

func TestGRPCSidecarClient_SourceUnavailable(t *testing.T) {
	c := startStubServer(t, &stubSidecarServer{
		verifyF: func(_ context.Context, _ *pb.VerifyRequest) (*pb.VerifyResponse, error) {
			return nil, status.Errorf(codes.Unavailable, "solana rpc unreachable")
		},
	})

	err := c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorIs(t, err, oracle.ErrSourceUnavailable)
	require.Contains(t, err.Error(), "sidecar unreachable")
}

func TestGRPCSidecarClient_Unreachable(t *testing.T) {
	// Grab a free port then close it so nothing is listening there.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	lis.Close()

	c, err := NewGRPCSidecarClient(addr)
	require.NoError(t, err) // gRPC dials lazily; construction always succeeds

	err = c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorIs(t, err, oracle.ErrSourceUnavailable)
	require.Contains(t, err.Error(), "sidecar unreachable")
}
