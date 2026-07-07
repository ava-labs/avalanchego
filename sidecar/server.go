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

// oracleVerifier is the domain interface Server depends on. Any implementation
// that can confirm or deny whether an OracleMessage occurred on its source chain
// satisfies this interface.
type oracleVerifier interface {
	Verify(ctx context.Context, msg *oracle.OracleMessage, justification []byte) error
}

// Server implements the OracleSidecar gRPC service. It routes each
// VerifyRequest to the verifier registered for the message's SourceType and
// maps errors to the gRPC status codes documented in proto/oracle/oracle.proto:
//
//   - codes.OK              — event confirmed on source chain
//   - codes.InvalidArgument — event cannot be confirmed (bad payload, unknown source type, …)
//   - codes.Unavailable     — source chain RPC is unreachable
type Server struct {
	pb.UnimplementedOracleSidecarServer
	verifiers map[string]oracleVerifier
}

// NewServer constructs a Server that dispatches to one verifier per source
// type. Messages whose SourceType is not present in verifiers are rejected
// with InvalidArgument.
func NewServer(verifiers map[string]oracleVerifier) *Server {
	return &Server{verifiers: verifiers}
}

func (s *Server) Verify(ctx context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	msg, err := oracle.ParseOracleMessage(req.MessageBytes)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse OracleMessage: %v", err)
	}

	v, ok := s.verifiers[msg.SourceType]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "no verifier registered for source type %q", msg.SourceType)
	}

	if err := v.Verify(ctx, msg, req.Justification); err != nil {
		if errors.Is(err, oracle.ErrSourceUnavailable) {
			return nil, status.Errorf(codes.Unavailable, "source chain unavailable: %v", err)
		}
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	return &pb.VerifyResponse{}, nil
}
