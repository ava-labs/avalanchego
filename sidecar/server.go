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

type oracleVerifier interface {
	Verify(ctx context.Context, msg *oracle.OracleMessage, justification []byte) error
}

// Server routes each VerifyRequest to the verifier registered for
// msg.SourceType. Unknown source types return InvalidArgument.
type Server struct {
	pb.UnimplementedOracleSidecarServer
	verifiers map[string]oracleVerifier
}

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
