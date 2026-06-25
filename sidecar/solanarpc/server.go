// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

// oracleVerifier is the domain interface Server depends on. The production
// implementation is *SolanaVerifier; tests may inject any compatible stub.
type oracleVerifier interface {
	Verify(ctx context.Context, msg *oracle.OracleMessage, justification []byte) error
}

// Server implements the OracleSidecar gRPC service. It decodes each
// VerifyRequest, forwards it to the oracleVerifier, and maps errors to the
// gRPC status codes documented in proto/oracle/oracle.proto:
//
//   - codes.OK            — event confirmed on Solana
//   - codes.InvalidArgument — event cannot be confirmed (bad payload, wrong slot, …)
//   - codes.Unavailable   — Solana RPC is unreachable
type Server struct {
	pb.UnimplementedOracleSidecarServer
	verifier oracleVerifier
}

func NewServer(v oracleVerifier) *Server {
	return &Server{verifier: v}
}

func (s *Server) Verify(ctx context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	msg, err := oracle.ParseOracleMessage(req.MessageBytes)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse OracleMessage: %v", err)
	}

	if err := s.verifier.Verify(ctx, msg, req.Justification); err != nil {
		if errors.Is(err, errSourceChainUnavailable) {
			return nil, status.Errorf(codes.Unavailable, "source chain unavailable: %v", err)
		}
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	return &pb.VerifyResponse{}, nil
}
